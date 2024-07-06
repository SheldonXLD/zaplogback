package zaplogback

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"

	"github.com/SheldonXLD/zaplogback/internal/bufferpool"
	"github.com/SheldonXLD/zaplogback/internal/pool"
)

// For JSON-escaping; see logbackEncoder.safeAddString below.
const _hex = "0123456789abcdef"
const _default_log_format = `%date{%Y-%m-%d %H:%M:%S.%3f} %level{upper} %caller %message %fields`
const _default_encoding_name = "zaplogback"

type EMPTY struct{}

var empty_member EMPTY

type LogbackConfig struct {
	EncodeLevel  zapcore.LevelEncoder
	EncodeTime   zapcore.TimeEncoder
	EncodeCaller zapcore.CallerEncoder
	// 新增format
	actions     []logActionOperation
	used_fields map[string]EMPTY
}

type logbackEncoder struct {
	*zapcore.EncoderConfig
	buf            *buffer.Buffer
	openNamespaces int

	// for encoding generic values by reflection
	reflectBuf *buffer.Buffer
	reflectEnc zapcore.ReflectedEncoder

	// 新增format
	actions     []logActionOperation
	used_fields map[string]EMPTY
}

type logActionOperation func(*logbackEncoder, *zapcore.Entry, []zapcore.Field)

var _logbackPool = pool.New(func() *logbackEncoder {
	return &logbackEncoder{}
})

func NewZaplogbackEncoder(cfg zapcore.EncoderConfig, log_format string) zapcore.Encoder {
	encoder := newZaplogbackEncoder(cfg)
	encoder.UseLogFormat(log_format)
	return encoder
}

func newZaplogbackEncoder(cfg zapcore.EncoderConfig) *logbackEncoder {
	if cfg.SkipLineEnding {
		cfg.LineEnding = ""
	} else if cfg.LineEnding == "" {
		cfg.LineEnding = "\n"
	}

	// If no EncoderConfig.NewReflectedEncoder is provided by the user, then use default
	if cfg.NewReflectedEncoder == nil {
		cfg.NewReflectedEncoder = defaultReflectedEncoder
	}

	return &logbackEncoder{
		EncoderConfig: &cfg,
		buf:           bufferpool.Get(),
	}
}

func RegisterLogbackEncoder(encoding string, logformat string) error {
	if logformat == "" {
		logformat = _default_log_format
	}

	if encoding == "" {
		encoding = _default_encoding_name
	}

	err := zap.RegisterEncoder(encoding, func(encoderConfig zapcore.EncoderConfig) (zapcore.Encoder, error) {
		return NewZaplogbackEncoder(encoderConfig, logformat), nil
	})

	if err != nil {
		return fmt.Errorf("encoding %q already exists", encoding)
	}
	return err
}

func putlogbackEncoder(enc *logbackEncoder) {
	if enc.reflectBuf != nil {
		enc.reflectBuf.Free()
	}
	enc.EncoderConfig = nil
	enc.buf = nil
	enc.openNamespaces = 0
	enc.reflectBuf = nil
	enc.reflectEnc = nil
	enc.actions = nil
	enc.used_fields = nil
	_logbackPool.Put(enc)
}

func (enc *logbackEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	final := enc.clone()

	for _, action := range enc.actions {
		action(final, &ent, fields)
	}

	if enc.buf.Len() > 0 {
		final.addElementSeparator()
		final.buf.Write(enc.buf.Bytes())
	}

	final.closeOpenNamespaces()
	if ent.Stack != "" && final.StacktraceKey != "" {
		// final.AddString(final.StacktraceKey, ent.Stack)
		final.buf.AppendByte('\n')
		final.buf.AppendBytes([]byte(ent.Stack))
	}
	final.buf.AppendString(final.LineEnding)

	ret := final.buf
	putlogbackEncoder(final)
	return ret, nil
}

func (enc *logbackEncoder) UseLogFormat(log_format string) {
	logback_config := Parse_compile_log_format(log_format)

	enc.actions = logback_config.actions
	enc.used_fields = logback_config.used_fields
	if logback_config.EncodeTime != nil {
		enc.EncodeTime = logback_config.EncodeTime
	}

	if logback_config.EncodeLevel != nil {
		enc.EncodeLevel = logback_config.EncodeLevel
	}

	if logback_config.EncodeCaller != nil {
		enc.EncodeCaller = logback_config.EncodeCaller
	}
}

func defaultReflectedEncoder(w io.Writer) zapcore.ReflectedEncoder {
	enc := json.NewEncoder(w)
	// For consistency with our custom JSON encoder.
	enc.SetEscapeHTML(false)
	return enc
}

func (enc *logbackEncoder) AddArray(key string, arr zapcore.ArrayMarshaler) error {
	enc.addKey(key)
	return enc.AppendArray(arr)
}

func (enc *logbackEncoder) AddObject(key string, obj zapcore.ObjectMarshaler) error {
	enc.addKey(key)
	return enc.AppendObject(obj)
}

func (enc *logbackEncoder) AddBinary(key string, val []byte) {
	enc.AddString(key, base64.StdEncoding.EncodeToString(val))
}

func (enc *logbackEncoder) AddByteString(key string, val []byte) {
	enc.addKey(key)
	enc.AppendByteString(val)
}

func (enc *logbackEncoder) AddBool(key string, val bool) {
	enc.addKey(key)
	enc.AppendBool(val)
}

func (enc *logbackEncoder) AddComplex128(key string, val complex128) {
	enc.addKey(key)
	enc.AppendComplex128(val)
}

func (enc *logbackEncoder) AddComplex64(key string, val complex64) {
	enc.addKey(key)
	enc.AppendComplex64(val)
}

func (enc *logbackEncoder) AddDuration(key string, val time.Duration) {
	enc.addKey(key)
	enc.AppendDuration(val)
}

func (enc *logbackEncoder) AddFloat64(key string, val float64) {
	enc.addKey(key)
	enc.AppendFloat64(val)
}

func (enc *logbackEncoder) AddFloat32(key string, val float32) {
	enc.addKey(key)
	enc.AppendFloat32(val)
}

func (enc *logbackEncoder) AddInt64(key string, val int64) {
	enc.addKey(key)
	enc.AppendInt64(val)
}

func (enc *logbackEncoder) resetReflectBuf() {
	if enc.reflectBuf == nil {
		enc.reflectBuf = bufferpool.Get()
		enc.reflectEnc = enc.NewReflectedEncoder(enc.reflectBuf)
	} else {
		enc.reflectBuf.Reset()
	}
}

var nullLiteralBytes = []byte("null")

// Only invoke the standard JSON encoder if there is actually something to
// encode; otherwise write JSON null literal directly.
func (enc *logbackEncoder) encodeReflected(obj interface{}) ([]byte, error) {
	if obj == nil {
		return nullLiteralBytes, nil
	}
	enc.resetReflectBuf()
	if err := enc.reflectEnc.Encode(obj); err != nil {
		return nil, err
	}
	enc.reflectBuf.TrimNewline()
	return enc.reflectBuf.Bytes(), nil
}

func (enc *logbackEncoder) AddReflected(key string, obj interface{}) error {
	valueBytes, err := enc.encodeReflected(obj)
	if err != nil {
		return err
	}
	enc.addKey(key)
	_, err = enc.buf.Write(valueBytes)
	return err
}

func (enc *logbackEncoder) OpenNamespace(key string) {
	enc.addKey(key)
	enc.buf.AppendByte('{')
	enc.openNamespaces++
}

func (enc *logbackEncoder) AddString(key, val string) {
	enc.addKey(key)
	enc.AppendString(val)
}

func (enc *logbackEncoder) AddTime(key string, val time.Time) {
	enc.addKey(key)
	enc.AppendTime(val)
}

func (enc *logbackEncoder) AddUint64(key string, val uint64) {
	enc.addKey(key)
	enc.AppendUint64(val)
}

func (enc *logbackEncoder) AppendArray(arr zapcore.ArrayMarshaler) error {
	enc.addElementSeparator()
	enc.buf.AppendByte('[')
	err := arr.MarshalLogArray(enc)
	enc.buf.AppendByte(']')
	return err
}

func (enc *logbackEncoder) AppendObject(obj zapcore.ObjectMarshaler) error {
	// Close ONLY new openNamespaces that are created during
	// AppendObject().
	old := enc.openNamespaces
	enc.openNamespaces = 0
	enc.addElementSeparator()
	enc.buf.AppendByte('{')
	err := obj.MarshalLogObject(enc)
	enc.buf.AppendByte('}')
	enc.closeOpenNamespaces()
	enc.openNamespaces = old
	return err
}

func (enc *logbackEncoder) AppendBool(val bool) {
	enc.addElementSeparator()
	enc.buf.AppendBool(val)
}

func (enc *logbackEncoder) AppendByteString(val []byte) {
	enc.addElementSeparator()
	// enc.buf.AppendByte('"')
	enc.safeAddByteString(val)
	// enc.buf.AppendByte('"')
}

// appendComplex appends the encoded form of the provided complex128 value.
// precision specifies the encoding precision for the real and imaginary
// components of the complex number.
func (enc *logbackEncoder) appendComplex(val complex128, precision int) {
	enc.addElementSeparator()
	// Cast to a platform-independent, fixed-size type.
	r, i := float64(real(val)), float64(imag(val))
	enc.buf.AppendByte('"')
	// Because we're always in a quoted string, we can use strconv without
	// special-casing NaN and +/-Inf.
	enc.buf.AppendFloat(r, precision)
	// If imaginary part is less than 0, minus (-) sign is added by default
	// by AppendFloat.
	if i >= 0 {
		enc.buf.AppendByte('+')
	}
	enc.buf.AppendFloat(i, precision)
	enc.buf.AppendByte('i')
	enc.buf.AppendByte('"')
}

func (enc *logbackEncoder) AppendDuration(val time.Duration) {
	cur := enc.buf.Len()
	if e := enc.EncodeDuration; e != nil {
		e(val, enc)
	}
	if cur == enc.buf.Len() {
		// User-supplied EncodeDuration is a no-op. Fall back to nanoseconds to keep
		// JSON valid.
		enc.AppendInt64(int64(val))
	}
}

func (enc *logbackEncoder) AppendInt64(val int64) {
	enc.addElementSeparator()
	enc.buf.AppendInt(val)
}

func (enc *logbackEncoder) AppendReflected(val interface{}) error {
	valueBytes, err := enc.encodeReflected(val)
	if err != nil {
		return err
	}
	enc.addElementSeparator()
	_, err = enc.buf.Write(valueBytes)
	return err
}

func (enc *logbackEncoder) AppendString(val string) {
	enc.safeAddString(val)
}

func (enc *logbackEncoder) AppendStringQuota(val string) {
	enc.addElementSeparator()
	enc.buf.AppendByte('"')
	enc.safeAddString(val)
	enc.buf.AppendByte('"')
}

// func (enc *logbackEncoder) AppendTimeLayout(time time.Time, layout string) {
// 	// enc.addElementSeparator()
// 	// enc.buf.AppendByte('"')
// 	enc.buf.AppendTime(time, layout)
// 	// enc.buf.AppendByte('"')
// }

func (enc *logbackEncoder) AppendTime(val time.Time) {
	cur := enc.buf.Len()
	if e := enc.EncodeTime; e != nil {
		e(val, enc)
	}
	if cur == enc.buf.Len() {
		// User-supplied EncodeTime is a no-op. Fall back to nanos since epoch to keep
		// output JSON valid.
		enc.AppendInt64(val.UnixNano())
	}
}

func (enc *logbackEncoder) AppendUint64(val uint64) {
	enc.addElementSeparator()
	enc.buf.AppendUint(val)
}

func (enc *logbackEncoder) AddInt(k string, v int)         { enc.AddInt64(k, int64(v)) }
func (enc *logbackEncoder) AddInt32(k string, v int32)     { enc.AddInt64(k, int64(v)) }
func (enc *logbackEncoder) AddInt16(k string, v int16)     { enc.AddInt64(k, int64(v)) }
func (enc *logbackEncoder) AddInt8(k string, v int8)       { enc.AddInt64(k, int64(v)) }
func (enc *logbackEncoder) AddUint(k string, v uint)       { enc.AddUint64(k, uint64(v)) }
func (enc *logbackEncoder) AddUint32(k string, v uint32)   { enc.AddUint64(k, uint64(v)) }
func (enc *logbackEncoder) AddUint16(k string, v uint16)   { enc.AddUint64(k, uint64(v)) }
func (enc *logbackEncoder) AddUint8(k string, v uint8)     { enc.AddUint64(k, uint64(v)) }
func (enc *logbackEncoder) AddUintptr(k string, v uintptr) { enc.AddUint64(k, uint64(v)) }
func (enc *logbackEncoder) AppendComplex64(v complex64)    { enc.appendComplex(complex128(v), 32) }
func (enc *logbackEncoder) AppendComplex128(v complex128)  { enc.appendComplex(complex128(v), 64) }
func (enc *logbackEncoder) AppendFloat64(v float64)        { enc.appendFloat(v, 64) }
func (enc *logbackEncoder) AppendFloat32(v float32)        { enc.appendFloat(float64(v), 32) }
func (enc *logbackEncoder) AppendInt(v int)                { enc.AppendInt64(int64(v)) }
func (enc *logbackEncoder) AppendInt32(v int32)            { enc.AppendInt64(int64(v)) }
func (enc *logbackEncoder) AppendInt16(v int16)            { enc.AppendInt64(int64(v)) }
func (enc *logbackEncoder) AppendInt8(v int8)              { enc.AppendInt64(int64(v)) }
func (enc *logbackEncoder) AppendUint(v uint)              { enc.AppendUint64(uint64(v)) }
func (enc *logbackEncoder) AppendUint32(v uint32)          { enc.AppendUint64(uint64(v)) }
func (enc *logbackEncoder) AppendUint16(v uint16)          { enc.AppendUint64(uint64(v)) }
func (enc *logbackEncoder) AppendUint8(v uint8)            { enc.AppendUint64(uint64(v)) }
func (enc *logbackEncoder) AppendUintptr(v uintptr)        { enc.AppendUint64(uint64(v)) }

func (enc *logbackEncoder) Clone() zapcore.Encoder {
	clone := enc.clone()
	clone.buf.Write(enc.buf.Bytes())
	return clone
}

func (enc *logbackEncoder) clone() *logbackEncoder {
	clone := _logbackPool.Get()
	clone.EncoderConfig = enc.EncoderConfig
	clone.used_fields = enc.used_fields
	clone.openNamespaces = enc.openNamespaces
	clone.buf = bufferpool.Get()
	return clone
}

func (enc *logbackEncoder) truncate() {
	enc.buf.Reset()
}

func (enc *logbackEncoder) closeOpenNamespaces() {
	for i := 0; i < enc.openNamespaces; i++ {
		enc.buf.AppendByte('}')
	}
	enc.openNamespaces = 0
}

func (enc *logbackEncoder) addKey(key string) {
	enc.addElementSeparator()
	enc.buf.AppendByte('"')
	enc.safeAddString(key)
	enc.buf.AppendByte('"')
	enc.buf.AppendByte(':')
}

func (enc *logbackEncoder) addElementSeparator() {
	last := enc.buf.Len() - 1
	if last < 0 {
		return
	}
	switch enc.buf.Bytes()[last] {
	case '{', '[', ':', ',', ' ':
		return
	default:
		enc.buf.AppendByte(' ')
	}
}

func (enc *logbackEncoder) appendFloat(val float64, bitSize int) {
	enc.addElementSeparator()
	switch {
	case math.IsNaN(val):
		enc.buf.AppendString(`"NaN"`)
	case math.IsInf(val, 1):
		enc.buf.AppendString(`"+Inf"`)
	case math.IsInf(val, -1):
		enc.buf.AppendString(`"-Inf"`)
	default:
		enc.buf.AppendFloat(val, bitSize)
	}
}

// safeAddString JSON-escapes a string and appends it to the internal buffer.
// Unlike the standard library's encoder, it doesn't attempt to protect the
// user from browser vulnerabilities or JSONP-related problems.
func (enc *logbackEncoder) safeAddString(s string) {
	safeAppendStringLike(
		(*buffer.Buffer).AppendString,
		utf8.DecodeRuneInString,
		enc.buf,
		s,
	)
}

// safeAddByteString is no-alloc equivalent of safeAddString(string(s)) for s []byte.
func (enc *logbackEncoder) safeAddByteString(s []byte) {
	safeAppendStringLike(
		(*buffer.Buffer).AppendBytes,
		utf8.DecodeRune,
		enc.buf,
		s,
	)
}

// safeAppendStringLike is a generic implementation of safeAddString and safeAddByteString.
// It appends a string or byte slice to the buffer, escaping all special characters.
func safeAppendStringLike[S []byte | string](
	// appendTo appends this string-like object to the buffer.
	appendTo func(*buffer.Buffer, S),
	// decodeRune decodes the next rune from the string-like object
	// and returns its value and width in bytes.
	decodeRune func(S) (rune, int),
	buf *buffer.Buffer,
	s S,
) {
	// The encoding logic below works by skipping over characters
	// that can be safely copied as-is,
	// until a character is found that needs special handling.
	// At that point, we copy everything we've seen so far,
	// and then handle that special character.
	//
	// last is the index of the last byte that was copied to the buffer.
	last := 0
	for i := 0; i < len(s); {
		if s[i] >= utf8.RuneSelf {
			// Character >= RuneSelf may be part of a multi-byte rune.
			// They need to be decoded before we can decide how to handle them.
			r, size := decodeRune(s[i:])
			if r != utf8.RuneError || size != 1 {
				// No special handling required.
				// Skip over this rune and continue.
				i += size
				continue
			}

			// Invalid UTF-8 sequence.
			// Replace it with the Unicode replacement character.
			appendTo(buf, s[last:i])
			buf.AppendString(`\ufffd`)

			i++
			last = i
		} else {
			// Character < RuneSelf is a single-byte UTF-8 rune.
			if s[i] >= 0x20 && s[i] != '\\' && s[i] != '"' {
				// No escaping necessary.
				// Skip over this character and continue.
				i++
				continue
			}

			// This character needs to be escaped.
			appendTo(buf, s[last:i])
			switch s[i] {
			case '\\', '"':
				buf.AppendByte('\\')
				buf.AppendByte(s[i])
			case '\n':
				buf.AppendByte('\\')
				buf.AppendByte('n')
			case '\r':
				buf.AppendByte('\\')
				buf.AppendByte('r')
			case '\t':
				buf.AppendByte('\\')
				buf.AppendByte('t')
			default:
				// Encode bytes < 0x20, except for the escape sequences above.
				buf.AppendString(`\u00`)
				buf.AppendByte(_hex[s[i]>>4])
				buf.AppendByte(_hex[s[i]&0xF])
			}

			i++
			last = i
		}
	}

	// add remaining
	appendTo(buf, s[last:])
}
