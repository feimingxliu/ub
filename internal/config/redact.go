package config

import "reflect"

// redactedMask is what every field tagged `secret:"true"` is replaced
// with when rendered for human inspection (e.g. `ub config show`).
const redactedMask = "***"

// Redact returns a deep copy of cfg with every string field whose Go
// struct tag includes `secret:"true"` replaced by redactedMask.
//
// Implementation: walk the value tree via reflection. The original cfg
// is left untouched so callers can keep using it.
func Redact(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	out := &Config{}
	redactValue(reflect.ValueOf(out).Elem(), reflect.ValueOf(*cfg))
	return out
}

// redactValue copies src into dst, replacing string fields whose
// owning struct tag has `secret:"true"`. Both values must be settable
// and of the same kind.
func redactValue(dst, src reflect.Value) {
	switch src.Kind() {
	case reflect.Struct:
		for i := 0; i < src.NumField(); i++ {
			field := src.Type().Field(i)
			srcField := src.Field(i)
			dstField := dst.Field(i)
			if field.Tag.Get("secret") == "true" && srcField.Kind() == reflect.String {
				if srcField.String() != "" {
					dstField.SetString(redactedMask)
				}
				continue
			}
			redactValue(dstField, srcField)
		}
	case reflect.Map:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.MakeMapWithSize(src.Type(), src.Len()))
		iter := src.MapRange()
		for iter.Next() {
			k := iter.Key()
			v := iter.Value()
			// Map values are not addressable; create a writable copy.
			newV := reflect.New(v.Type()).Elem()
			redactValue(newV, v)
			dst.SetMapIndex(k, newV)
		}
	case reflect.Slice:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.MakeSlice(src.Type(), src.Len(), src.Cap()))
		for i := 0; i < src.Len(); i++ {
			redactValue(dst.Index(i), src.Index(i))
		}
	case reflect.Ptr:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.New(src.Type().Elem()))
		redactValue(dst.Elem(), src.Elem())
	case reflect.Interface:
		// `Unknown map[string]any` lands here. Copy values as-is - the
		// Unknown bag holds forward-compat fields, not secrets.
		if !src.IsNil() {
			dst.Set(src)
		}
	default:
		if src.IsValid() && dst.CanSet() {
			dst.Set(src)
		}
	}
}
