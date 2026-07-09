package format

// Parquet metadata extraction. Reads only the file footer (FileMetaData,
// thrift compact protocol) — never the data pages — so extraction cost is
// independent of file size. See ADR-0003.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/joeymeij/dit/internal/model"
)

const parquetMagic = "PAR1"

// maxFooterSize guards against reading absurd footer lengths from
// corrupt files (Parquet footers are typically kilobytes).
const maxFooterSize = 128 << 20

// ErrNotParquet is returned for files without valid Parquet framing.
var ErrNotParquet = errors.New("not a parquet file")

// ParquetDetector handles .parquet files.
type ParquetDetector struct{}

func init() { Register(ParquetDetector{}) }

// Detect matches on file extension.
func (ParquetDetector) Detect(path string) bool { return ext(path) == "parquet" }

// Extract reads schema and row count from the footer.
func (ParquetDetector) Extract(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	footer, err := readFooter(f, fi.Size())
	if err != nil {
		return Info{}, err
	}
	md, err := parseFileMetaData(footer)
	if err != nil {
		return Info{}, err
	}
	return Info{
		Format: "parquet",
		Rows:   md.numRows,
		Cols:   len(md.schema.Columns),
		Schema: md.schema,
	}, nil
}

// readFooter validates Parquet framing and returns the raw FileMetaData
// bytes. Layout of a Parquet file tail: [metadata][4-byte LE length]["PAR1"].
func readFooter(r io.ReaderAt, size int64) ([]byte, error) {
	if size < 12 { // magic + length + magic is the minimum possible file
		return nil, ErrNotParquet
	}
	tail := make([]byte, 8)
	if _, err := r.ReadAt(tail, size-8); err != nil {
		return nil, fmt.Errorf("reading parquet tail: %w", err)
	}
	if string(tail[4:]) != parquetMagic {
		return nil, ErrNotParquet
	}
	footerLen := int64(binary.LittleEndian.Uint32(tail[:4]))
	if footerLen <= 0 || footerLen > maxFooterSize || footerLen > size-12 {
		return nil, fmt.Errorf("%w: invalid footer length %d", ErrNotParquet, footerLen)
	}
	footer := make([]byte, footerLen)
	if _, err := r.ReadAt(footer, size-8-footerLen); err != nil {
		return nil, fmt.Errorf("reading parquet footer: %w", err)
	}
	return footer, nil
}

type fileMetaData struct {
	numRows int64
	schema  *model.Schema
}

// schemaElement is the subset of parquet.thrift's SchemaElement we need.
type schemaElement struct {
	typ           int64 // parquet physical type enum; -1 if absent
	name          string
	numChildren   int64
	convertedType int64 // parquet ConvertedType enum; -1 if absent
	hasLogical    bool
	logicalKind   int16 // field ID inside LogicalType union (e.g. 5=DATE)
}

// parseFileMetaData decodes FileMetaData, keeping fields:
//
//	2: list<SchemaElement> schema
//	3: i64 num_rows
func parseFileMetaData(buf []byte) (fileMetaData, error) {
	r := &thriftReader{buf: buf}
	md := fileMetaData{schema: &model.Schema{}}
	var elements []schemaElement

	var last int16
	for {
		id, typ, done, err := r.fieldHeader(last)
		if err != nil {
			return md, fmt.Errorf("parquet footer: %w", err)
		}
		if done {
			break
		}
		switch {
		case id == 2 && typ == tList:
			elemType, size, err := r.listHeader()
			if err != nil {
				return md, err
			}
			if elemType != tStruct {
				return md, fmt.Errorf("parquet schema: unexpected element type %d", elemType)
			}
			for i := 0; i < size; i++ {
				el, err := parseSchemaElement(r)
				if err != nil {
					return md, err
				}
				elements = append(elements, el)
			}
		case id == 3 && typ == tI64:
			n, err := r.varint()
			if err != nil {
				return md, err
			}
			md.numRows = n
		default:
			if err := r.skip(typ); err != nil {
				return md, fmt.Errorf("parquet footer field %d: %w", id, err)
			}
		}
		last = id
	}
	md.schema = schemaFromElements(elements)
	return md, nil
}

// parseSchemaElement decodes one SchemaElement struct, keeping fields:
//
//	1: Type type   4: string name   5: i32 num_children
//	6: ConvertedType converted_type   10: LogicalType logicalType
func parseSchemaElement(r *thriftReader) (schemaElement, error) {
	el := schemaElement{typ: -1, convertedType: -1}
	var last int16
	for {
		id, typ, done, err := r.fieldHeader(last)
		if err != nil {
			return el, fmt.Errorf("parquet schema element: %w", err)
		}
		if done {
			return el, nil
		}
		switch {
		case id == 1 && typ == tI32:
			el.typ, err = r.varint()
		case id == 4 && typ == tBinary:
			var b []byte
			b, err = r.binary()
			el.name = string(b)
		case id == 5 && typ == tI32:
			el.numChildren, err = r.varint()
		case id == 6 && typ == tI32:
			el.convertedType, err = r.varint()
		case id == 10 && typ == tStruct:
			el.hasLogical = true
			el.logicalKind, err = unionFieldID(r)
		default:
			err = r.skip(typ)
		}
		if err != nil {
			return el, fmt.Errorf("parquet schema element field %d: %w", id, err)
		}
		last = id
	}
}

// unionFieldID reads a thrift union (a struct with exactly one set
// field) and returns which field was set, skipping its value.
func unionFieldID(r *thriftReader) (int16, error) {
	var set int16
	var last int16
	for {
		id, typ, done, err := r.fieldHeader(last)
		if err != nil {
			return 0, err
		}
		if done {
			return set, nil
		}
		if set == 0 {
			set = id
		}
		if err := r.skip(typ); err != nil {
			return 0, err
		}
		last = id
	}
}

// Parquet physical type enum (parquet.thrift Type).
const (
	pqBoolean           = 0
	pqInt32             = 1
	pqInt64             = 2
	pqInt96             = 3
	pqFloat             = 4
	pqDouble            = 5
	pqByteArray         = 6
	pqFixedLenByteArray = 7
)

// ConvertedType values we map to richer dit types.
const (
	ctUTF8            = 0
	ctDate            = 6
	ctTimestampMillis = 9
	ctTimestampMicros = 10
)

// LogicalType union field IDs (parquet.thrift LogicalType).
const (
	ltDate      = 5
	ltTimestamp = 8
)

// schemaFromElements converts the flattened depth-first SchemaElement
// list into dit's column schema. The first element is the schema
// root; its direct children become the columns. Nested groups are
// reported as type "nested".
func schemaFromElements(elements []schemaElement) *model.Schema {
	sch := &model.Schema{}
	if len(elements) < 2 {
		return sch
	}
	// Walk the root's direct children, skipping each child's subtree.
	i := 1
	for i < len(elements) {
		el := elements[i]
		sch.Columns = append(sch.Columns, model.Column{Name: el.name, Type: columnType(el)})
		i += 1 + subtreeSize(elements, i)
	}
	return sch
}

// subtreeSize returns how many elements after index i belong to the
// subtree rooted at elements[i].
func subtreeSize(elements []schemaElement, i int) int {
	n := 0
	children := int(elements[i].numChildren)
	for c := 0; c < children; c++ {
		if i+1+n >= len(elements) {
			break // malformed schema list; stop rather than panic
		}
		n += 1 + subtreeSize(elements, i+1+n)
	}
	return n
}

// columnType maps a parquet element to a dit column type.
func columnType(el schemaElement) string {
	if el.numChildren > 0 {
		return "nested"
	}
	if el.hasLogical {
		switch el.logicalKind {
		case ltDate:
			return "date"
		case ltTimestamp:
			return "datetime"
		}
	}
	switch el.convertedType {
	case ctDate:
		return "date"
	case ctTimestampMillis, ctTimestampMicros:
		return "datetime"
	case ctUTF8:
		return "string"
	}
	switch el.typ {
	case pqBoolean:
		return "boolean"
	case pqInt32, pqInt64, pqInt96:
		return "integer"
	case pqFloat, pqDouble:
		return "float"
	case pqByteArray, pqFixedLenByteArray:
		return "string"
	default:
		return "string"
	}
}
