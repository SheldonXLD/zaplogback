package zaplogback

import (
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap/zapcore"
)

func logAddBytesAction(value []byte) logActionOperation {
	return func(final *logbackEncoder, ent *zapcore.Entry, fields []zapcore.Field) {
		final.buf.AppendBytes(value)
	}
}

func logAddTimeAction(final *logbackEncoder, ent *zapcore.Entry, fields []zapcore.Field) {
	if final.TimeKey != "" && !ent.Time.IsZero() {
		final.AppendTime(ent.Time)
	}
}

func logAddLevelAction(final *logbackEncoder, ent *zapcore.Entry, fields []zapcore.Field) {
	if final.LevelKey != "" && final.EncodeLevel != nil {
		cur := final.buf.Len()
		final.EncodeLevel(ent.Level, final)
		if cur == final.buf.Len() {
			// User-supplied EncodeLevel was a no-op. Fall back to strings to keep
			// output JSON valid.
			final.buf.AppendBytes([]byte(ent.Level.String()))
			// final.AppendByteString([]byte(ent.Level.String()))
		}
	}
}

func logAddCallerAction(final *logbackEncoder, ent *zapcore.Entry, fields []zapcore.Field) {

	if ent.Caller.Defined {
		if final.CallerKey != "" {
			// final.addKey(final.CallerKey)
			cur := final.buf.Len()
			final.EncodeCaller(ent.Caller, final)
			if cur == final.buf.Len() {
				// User-supplied EncodeCaller was a no-op. Fall back to strings to
				// keep output JSON valid.
				final.buf.AppendBytes([]byte(ent.Caller.String()))
			}
		}
		if final.FunctionKey != "" {
			// final.addKey(final.FunctionKey)
			// final.safeAddString(ent.Caller.Function)
			final.buf.AppendBytes([]byte(ent.Caller.Function))
		}
	}
}

func logAddNameAction(final *logbackEncoder, ent *zapcore.Entry, fields []zapcore.Field) {
	if ent.LoggerName != "" && final.NameKey != "" {
		final.addKey(final.NameKey)
		cur := final.buf.Len()
		nameEncoder := final.EncodeName

		// if no name encoder provided, fall back to FullNameEncoder for backwards
		// compatibility
		if nameEncoder == nil {
			nameEncoder = zapcore.FullNameEncoder
		}

		nameEncoder(ent.LoggerName, final)
		if cur == final.buf.Len() {
			// User-supplied EncodeName was a no-op. Fall back to strings to
			// keep output JSON valid.
			final.buf.AppendBytes([]byte(ent.LoggerName))
		}
	}
}

func logAddMsgAction(final *logbackEncoder, ent *zapcore.Entry, fields []zapcore.Field) {
	if final.MessageKey != "" {
		final.buf.AppendBytes([]byte(ent.Message))
	}
}

func logAddUsedFieldAction(field string, before_byte []byte, after_byte []byte) logActionOperation {
	return func(final *logbackEncoder, ent *zapcore.Entry, fields []zapcore.Field) {
		for _, f := range fields {
			if field == f.Key {
				final.buf.AppendBytes(before_byte)
				final.buf.AppendBytes([]byte(f.String))
				final.buf.AppendBytes(after_byte)
			}
		}
	}
}

func logAddRemindFieldAction(final *logbackEncoder, ent *zapcore.Entry, fields []zapcore.Field) {

	final.buf.AppendByte('{')
	comma_need := false
	for _, field := range fields {
		_, exists := final.used_fields[field.Key]
		if exists {
			continue
		}
		if comma_need {
			final.buf.AppendByte(',')
			comma_need = false
		}
		field.AddTo(final)
		comma_need = true
	}
	final.buf.AppendByte('}')
}

// log_format := `%date{2024-12-01 12:34:56.789} %level{upper} %caller ["tid":%x{tid}]} %message %fields`
func Parse_compile_log_format(log_format string) LogbackConfig {
	log_format_regex_pattern := regexp.MustCompile(`(?P<action>%\w+)(?P<config>\{.*?\})?(?P<remind>[^%]*)`)

	named_idx := make(map[string]int)
	for idx, name := range log_format_regex_pattern.SubexpNames() {
		if idx == 0 {
			continue
		}
		named_idx[name] = idx
	}

	all_matches := log_format_regex_pattern.FindAllSubmatch([]byte(log_format), -1)

	config_regex_pattern := regexp.MustCompile(`\{(.*)\}`)
	x_config_regex_pattern := regexp.MustCompile(`(\w+)(:(.*)(\$0)(.*))?`)
	var logback_config LogbackConfig
	action_ops := []logActionOperation{}
	used_fields := make(map[string]EMPTY)

	for _, m := range all_matches {
		action_config := ""

		action := m[named_idx["action"]]
		config := m[named_idx["config"]]
		remind := m[named_idx["remind"]]

		config_match := config_regex_pattern.FindSubmatch(config)

		if len(config_match) > 0 {
			action_config = string(config_match[1])
		}

		switch string(action) {
		case `%date`:
			if len(action_config) > 0 {
				// 自定义时间格式
				logback_config.EncodeTime = TimeEncoderOf(action_config)
			}
			action_ops = append(action_ops, logAddTimeAction)

		case `%level`:
			if len(action_config) > 0 {
				logback_config.EncodeLevel = LevelEncoderOf(action_config)
			}
			action_ops = append(action_ops, logAddLevelAction)
		case `%caller`:
			if len(action_config) > 0 {
				logback_config.EncodeCaller = CallerEncoderOf(action_config)
			}

			action_ops = append(action_ops, logAddCallerAction)
		case `%message`:
			action_ops = append(action_ops, logAddMsgAction)
		case `%x`:
			if len(action_config) > 0 {
				// 从fields 中取出自定义变量
				adv_config := x_config_regex_pattern.FindSubmatch([]byte(action_config))
				used_field_name := adv_config[1]
				used_fields[string(used_field_name)] = empty_member
				before_field_str := adv_config[3]
				after_field_str := adv_config[5]

				action_ops = append(action_ops, logAddUsedFieldAction(string(used_field_name), before_field_str, after_field_str))
			}
		case `%fields`:
			action_ops = append(action_ops, logAddRemindFieldAction)
		default:
			// 都当成是普通字符串处理
			remind = append(remind, action...)
		}

		if len(remind) >= 1 {
			action_ops = append(action_ops, logAddBytesAction(remind))
		}
	}

	logback_config.actions = action_ops
	logback_config.used_fields = used_fields

	return logback_config
}

const _default_milesecond_zero_count = 3

func LevelEncoderOf(level_type string) zapcore.LevelEncoder {
	switch level_type {
	case "upper":
		fallthrough
	case "capital":
		return zapcore.CapitalLevelEncoder
	case "capitalcolor":
		return zapcore.CapitalColorLevelEncoder
	case "color":
		return zapcore.LowercaseColorLevelEncoder
	case "lower":
		fallthrough
	default:
		return zapcore.LowercaseLevelEncoder
	}
}

func CallerEncoderOf(caller_type string) zapcore.CallerEncoder {
	switch caller_type {
	case "full":
		return zapcore.FullCallerEncoder
	default:
		return zapcore.ShortCallerEncoder
	}
}

func StrftimeFormatLayout(layout string) string {

	go_time_format := layout

	strf_map := map[string]string{
		`%a`:      "Mon",     // 星期 简称
		`%A`:      "Monday",  // 星期 全称
		`%w`:      "_2",      // 日 星期的第几天
		`%d`:      "02",      // 日 月的第几天
		`%b`:      "Jan",     // 月 简称
		`%B`:      "January", // 月 全称
		`%m`:      "01",      // 月 数字
		`%y`:      "06",      // 年 两位
		`%Y`:      "2006",    // 年 四位
		`%H`:      "15",      // 时 24小时制
		`%I`:      "03",      // 时 12小时制
		`%p`:      "PM",      // 上午or下午
		`%M`:      "04",      // 分钟
		`%S`:      "05",      // 秒
		`%(\d*)f`: "000",     // 毫秒
		`%z`:      "-0700",   // 时区（数字表示）
		`%Z`:      "MST",     // 时区（英文表示）
		`%j`:      "__2",     // 日 年的第几天
	}

	for k, v := range strf_map {
		pattern := regexp.MustCompile(k)
		if k == `%(\d*)f` {
			zero_count, err := strconv.Atoi(string(pattern.FindSubmatch([]byte(go_time_format))[1]))
			if err != nil {
				zero_count = _default_milesecond_zero_count
			}
			v = strings.Repeat("0", zero_count)
		}
		go_time_format = pattern.ReplaceAllString(go_time_format, v)

	}

	return go_time_format
}

func TimeEncoderOf(layout string) zapcore.TimeEncoder {
	go_time_format := StrftimeFormatLayout(layout)
	return zapcore.TimeEncoderOfLayout(go_time_format)
}
