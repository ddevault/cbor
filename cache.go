package cbor

import (
	"bytes"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	decodingStructTypeCache sync.Map // map[reflect.Type]decodingStructType
	encodingStructTypeCache sync.Map // map[reflect.Type]encodingStructType
	encodeFuncCache         sync.Map // map[reflect.Type]encodeFunc
)

type decodingStructType struct {
	fields  fields
	toArray bool
}

func getDecodingStructType(t reflect.Type) decodingStructType {
	if v, _ := decodingStructTypeCache.Load(t); v != nil {
		return v.(decodingStructType)
	}

	flds, structOptions := getFields(t)

	toArray := hasToArrayOption(structOptions)

	for i := 0; i < len(flds); i++ {
		flds[i].isUnmarshaler = implementsUnmarshaler(flds[i].typ)
	}

	structType := decodingStructType{fields: flds, toArray: toArray}
	decodingStructTypeCache.Store(t, structType)
	return structType
}

type encodingStructType struct {
	fields            fields
	canonicalFields   fields
	err               error
	toArray           bool
	omitEmpty         bool
	hasAnonymousField bool
}

// byCanonical sorts fields by CBOR encoded field name.
type byCanonical struct {
	fields
}

func (x byCanonical) Less(i, j int) bool {
	return bytes.Compare(x.fields[i].cborName, x.fields[j].cborName) <= 0
}

func getEncodingStructType(t reflect.Type) encodingStructType {
	if v, _ := encodingStructTypeCache.Load(t); v != nil {
		return v.(encodingStructType)
	}

	flds, structOptions := getFields(t)

	toArray := hasToArrayOption(structOptions)

	var err error
	var omitEmpty bool
	var hasAnonymousField bool
	e := getEncodeState()
	for i := 0; i < len(flds); i++ {
		// Get field's encodeFunc
		flds[i].ef = getEncodeFunc(flds[i].typ)
		if flds[i].ef == nil {
			err = &UnsupportedTypeError{t}
			break
		}

		// Encode field name
		if !toArray {
			if flds[i].keyAsInt {
				nameAsInt, numErr := strconv.Atoi(flds[i].name)
				if numErr != nil {
					err = numErr
					break
				}
				if nameAsInt >= 0 {
					encodeTypeAndAdditionalValue(e, byte(cborTypePositiveInt), uint64(nameAsInt))
				} else {
					n := nameAsInt*(-1) - 1
					encodeTypeAndAdditionalValue(e, byte(cborTypeNegativeInt), uint64(n))
				}
				flds[i].cborName = make([]byte, e.Len())
				copy(flds[i].cborName, e.Bytes())
				e.Reset()
			} else {
				encodeTypeAndAdditionalValue(e, byte(cborTypeTextString), uint64(len(flds[i].name)))
				flds[i].cborName = make([]byte, e.Len()+len(flds[i].name))
				n := copy(flds[i].cborName, e.Bytes())
				copy(flds[i].cborName[n:], flds[i].name)
				e.Reset()
			}
		}

		// Check if field is from embedded struct
		if !hasAnonymousField && len(flds[i].idx) > 1 {
			hasAnonymousField = true
		}

		// Check if field can be omitted when empty
		if !omitEmpty && flds[i].omitEmpty {
			omitEmpty = true
		}
	}
	putEncodeState(e)

	if err != nil {
		structType := encodingStructType{err: err}
		encodingStructTypeCache.Store(t, structType)
		return structType
	}

	// Sort fields by canonical order
	canonicalFields := make(fields, len(flds))
	copy(canonicalFields, flds)
	sort.Sort(byCanonical{canonicalFields})

	structType := encodingStructType{fields: flds, canonicalFields: canonicalFields, toArray: toArray, omitEmpty: omitEmpty, hasAnonymousField: hasAnonymousField}
	encodingStructTypeCache.Store(t, structType)
	return structType
}

func getEncodeFunc(t reflect.Type) encodeFunc {
	if v, _ := encodeFuncCache.Load(t); v != nil {
		return v.(encodeFunc)
	}
	f := getEncodeFuncInternal(t)
	encodeFuncCache.Store(t, f)
	return f
}

func hasToArrayOption(tag string) bool {
	s := ",toarray"
	idx := strings.Index(tag, s)
	return idx >= 0 && (len(tag) == idx+len(s) || tag[idx+len(s)] == ',')
}
