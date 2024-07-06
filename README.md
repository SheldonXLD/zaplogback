# zaplogback

Make the zaplog output log format more flexible, you can control the order of log keys and fields, control datetime format by strftime codes, etc.

logback_encoder.go is inspired by zap's json_encoder.go and console_encoder.go

zaplogback 可以灵活调整不同日志字段的输出格式顺序；根据strftime格式定义日期格式；自定义Field的输出格式等

ref: 

[zaplog](https://github.com/uber-go/zap)

# usage

## setup example

All you need, just Register a new encoding to zaplog. Every format need a new encoding.

Then you use new **Encoding** in zap's Config

每种格式，必须单独注册一个新的Encoder

````go
    log_format := `%date{%Y-%m-%d %H:%M:%S.%3f} %level{upper} %caller %x{tid:["tid":$0]} %message %fields`
    zap_encoding_name := "zaplogback"

	err := zaplogback.RegisterLogbackEncoder(zap_encoding_name, log_format)

	if err != nil {
		fmt.Println(err)
		return
	}

	cfg := zap.Config{
		Level:       zap.NewAtomicLevelAt(zap.InfoLevel),
		Development: false,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         zap_encoding_name,
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}
	logger, _ := cfg.Build()


    // 2024-07-06 20:32:18.335 INFO test/main.go:35 ["tid":abcd-efghi-jkl] this is a test log {"otherfields":otherfields value}
	logger.Info("this is a test log", zap.String("tid", "abcd-efghi-jkl"), zap.String("otherfields", "otherfields value"))
````

## log_format intro

| action   | desc                              |
| -------- | --------------------------------- |
| %date    | strftime format datetime          |
| %level   | log level                         |
| %caller  | file function and line            |
| %message | message                           |
| %x       | advantange output format of field |
| %fields  | fields                            |

日志将按照不同的action出现顺序进行输出，对部分action, 可以进一步定义配置，比如日期格式，level 是否大写等

You can change any order of **action** to control log output format

syntax:  %action{config}

config is optional

### date

根据strftime code 定义日期格式

date format configs, use strftime format

example:

%date{%Y-%m-%d %H:%M:%S.%3f}    output: 2024-07-31 12:34:56.789

ref:

[python's datetime strftime format](https://docs.python.org/3/library/datetime.html#strftime-and-strptime-format-codes)

````go
		`%a`:      "Mon",     // 星期 简称          weekday abbr
		`%A`:      "Monday",  // 星期 全称          weekday fullname
		`%w`:      "_2",      // 日 星期的第几天     day of week    
		`%d`:      "02",      // 日 月的第几天       day of month
		`%b`:      "Jan",     // 月 简称            month abbr
		`%B`:      "January", // 月 全称            month fullname
		`%m`:      "01",      // 月 数字            month by number
		`%y`:      "06",      // 年 两位            year's last two number
		`%Y`:      "2006",    // 年 四位            year
		`%H`:      "15",      // 时 24小时制        hour by 24
		`%I`:      "03",      // 时 12小时制        hour by 12
		`%p`:      "PM",      // 上午or下午         AM or PM
		`%M`:      "04",      // 分钟               minutes
		`%S`:      "05",      // 秒                 seconds
		`%(\d*)f`: "000",     // 毫秒               mileseconds or nanoseconds
		`%z`:      "-0700",   // 时区（数字表示）    time zone number
		`%Z`:      "MST",     // 时区（英文表示）    time zone name
		`%j`:      "__2",     // 日 年的第几天       day of year

````
其中毫秒的定义中，实际上是对纳秒的截断，有效范围 1-9， 其中 %f 等同于 %3f, 代表毫秒

in mileseconds settings, %f is equal %3f by default, valid number is [1, 9]

````go
	date_format := zaplogback.StrftimeFormatLayout("%Y-%m-%d %H:%M:%S.%3f %a %A %w %b %B %y %I %p %z %Z %j")
	fmt.Println(date_format)
	now := time.Now().Format(date_format)
	fmt.Println(now)

2006-01-02 15:04:05.000 Mon Monday _2 Jan January 06 03 PM -0700 MST __2
2024-07-06 20:50:58.892 Sat Saturday  6 Jul July 24 08 PM +0800 CST 188

````

### level

日志级别，使用原生的zaplog LevelEncoder

same as zaplog's default log encoder:

example:

%level            lower case   info

%level{upper}     upper case   INFO

%level{capital}   upper case   INFO

%level{lower}     lower case   info

### caller

使用原生的 zaplog CallerEncoder

same as zaplog's default caller encoder

example: 

%caller              defualt, short filepath of caller

%caller{full}        full filepath of caller

### message

just message, no config

example:

%message

### x

对于field的高级输出定义， 若进行高级定义，必须包含占位符 **$0**

advantage format output of fields. If need advantage format out, Must contains **$0**

if field is zap.String("tid", "abcd-efghi-jkl")

syntax:
%x{fieldName:AdvantageFormatOutput}

fieldName: must 必选

AdvantageFormatOutput: optional 可选

example:
%x{tid}             output: abcd-efghi-jkl

%x{tid:["tid":$0]}    output: ["tid":abcd-efghi-jkl]   // $0 match, 按照格式进行输出

%x{tid:"tid"}       output: abcd-efghi-jkl         // not match $0, 只输出 field

### fields

输出排除 %x 定义的 fields

output fields exclude %x{...}