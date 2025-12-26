package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"time"

	otelpyroscope "github.com/grafana/otel-profiling-go"
	"github.com/grafana/pyroscope-go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otel_log "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// 一些常量，方便在资源标签、日志等位置保持一致
const (
	serviceName    = "demo-app"
	serviceVersion = "1.0.0"
	jobName        = "demo-app"

	routeFast  = "/hello"
	routeSlow  = "/slow"
	routeAlloc = "/alloc"
)

// 全局 Telemetry 组件
var (
	tracer = otel.Tracer(serviceName)
	meter  = otel.Meter(serviceName)
	logger = global.Logger(serviceName)

	// 用于模拟“持续占用内存”的场景（轻量级版内存泄漏 demo）
	allocHolder [][]byte

	// 模拟“写得不合理的邮箱校验正则”，用于制造 CPU 压力
	//
	// 注意：Go 使用 RE2 引擎，本身不会真的卡死或指数级回溯，
	// 这里只是通过高频重复匹配一段复杂字符串，来产生可观的 CPU 消耗。
	emailRegexp = regexp.MustCompile(`^(\w+([-.][A-Za-z0-9]+)*){3,18}@\w+([-.][A-Za-z0-9]+)*\.\w+([-.][A-Za-z0-9]+)*$`)
	// 这个邮箱字符串在很多 PCRE/PCRE2 引擎下会触发“灾难性回溯”，
	// 在本 demo 中我们用它来做高开销的匹配目标。
	slowEmailSample = "rosamariachoccelahuaaranda70@gmail.comnnbbb.bbNG.bbb.n¿.?n"

	// 演示用业务指标
	requestCount metric.Int64Counter
	histogram    metric.Float64Histogram
)

// initProvider 初始化 OTel 的 Traces / Metrics / Logs 三种信号，并返回统一的关闭函数。
func initProvider(ctx context.Context) (func(context.Context) error, error) {
	// 1. 读取 OTLP 采集端地址（Alloy）
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "localhost:4318"
	}

	// 2. 公共资源属性：所有信号都会带上这些标签
	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", serviceVersion),
			// 下面两个是为了和 Loki / Mimir 查询里的 label 对齐
			attribute.String("service_name", serviceName),
			attribute.String("job", jobName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// === Traces ===
	traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(otlpEndpoint+"/v1/traces"))
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}
	// 原始 OTel TracerProvider
	baseTP := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	// 使用 otel-profiling-go 包装 TracerProvider，让 span 自动携带 pyroscope.profile.id 等 Profiling 关联信息
	tp := otelpyroscope.NewTracerProvider(baseTP)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	// === Metrics ===
	metricExporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(otlpEndpoint+"/v1/metrics"))
	if err != nil {
		return nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// === Logs ===
	logExporter, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(otlpEndpoint+"/v1/logs"))
	if err != nil {
		return nil, fmt.Errorf("failed to create log exporter: %w", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)
	global.SetLoggerProvider(lp)

	return func(ctx context.Context) error {
		// 注意：tp 由 otel-profiling-go 包装，真正需要 Shutdown 的是底层的 baseTP
		_ = baseTP.Shutdown(ctx)
		_ = mp.Shutdown(ctx)
		_ = lp.Shutdown(ctx)
		return nil
	}, nil
}

// startPyroscope 启动 Pyroscope 客户端，将 CPU / 内存 Profile 推送到 Pyroscope 服务。
func startPyroscope() error {
	pyroscopeAddr := os.Getenv("PYROSCOPE_SERVER_ADDRESS")
	if pyroscopeAddr == "" {
		pyroscopeAddr = "http://localhost:4040"
	}
	_, err := pyroscope.Start(pyroscope.Config{
		// ApplicationName 会出现在 Pyroscope 的 service 下拉框里
		ApplicationName: serviceName,
		ServerAddress:   pyroscopeAddr,
		Logger:          nil,
		Tags:            map[string]string{"env": "dev"},
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,
		},
	})
	return err
}

func main() {
	ctx := context.Background()

	// 1. 初始化 OTel 管道（Traces / Metrics / Logs）
	shutdown, err := initProvider(ctx)
	if err != nil {
		log.Fatalf("failed to init OTel: %v", err)
	}
	defer shutdown(ctx)

	// 2. 启动 Pyroscope（持续性能分析）
	if err := startPyroscope(); err != nil {
		log.Printf("failed to start pyroscope: %v", err)
	}

	// 3. 初始化业务指标
	if err := initMetrics(); err != nil {
		log.Fatalf("failed to init metrics: %v", err)
	}

	// 4. 启动 HTTP Server：提供 /hello 和 /slow 两个测试接口
	startHTTPServer()

	// 5. 启动流量生成循环：不断调用 /slow，制造 Trace + Profile 数据
	startTrafficGenerator(ctx)
}

// initMetrics 定义简单的请求计数 + 耗时直方图指标。
func initMetrics() error {
	var err error

	requestCount, err = meter.Int64Counter(
		"demo_request_total",
		metric.WithDescription("Total requests"),
	)
	if err != nil {
		return err
	}

	histogram, err = meter.Float64Histogram(
		"demo_request_duration_seconds",
		metric.WithDescription("Request duration in seconds"),
	)
	return err
}

// startHTTPServer 注册路由并启动 HTTP 服务。
// - /hello：模拟快速接口（轻量逻辑）
// - /slow：模拟 CPU 慢接口（正则匹配）
// - /alloc：模拟“内存占用/泄漏”接口（大量分配并缓存在全局切片）
func startHTTPServer() {
	// /hello: 快速、轻量级请求
	http.Handle(routeFast, otelhttp.NewHandler(http.HandlerFunc(helloHandler), "Hello"))
	// /slow: 人为制造的“慢接口”，CPU 占用明显，方便在 Traces -> Profiles 里演示
	http.Handle(routeSlow, otelhttp.NewHandler(http.HandlerFunc(slowHandler), "Slow"))
	// /alloc: 模拟一次请求导致大量内存分配的场景
	http.Handle(routeAlloc, otelhttp.NewHandler(http.HandlerFunc(allocHandler), "Alloc"))

	go func() {
		log.Println("HTTP server listening on :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatal(err)
		}
	}()
}

// startTrafficGenerator 持续向 /slow 发请求，模拟外部调用流量。
// 这个函数内部会：
// - 为每次请求创建一个顶层 span：traffic_generator_request
// - 在 span 中制造一定 CPU 负载（fib(25)）
// - 调用 /slow 接口，让下游服务继续产生 span + profile
func startTrafficGenerator(ctx context.Context) {
	log.Println("Starting traffic generator...")
	client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}

	for {
		// 为每一次“调用下游服务”创建一个新的顶层 span
		// 在 Tempo 中你会看到它的名字是 traffic_generator_request
		iterCtx, span := tracer.Start(ctx, "traffic_generator_request")
		span.SetAttributes(
			attribute.String("job", jobName),
			attribute.String("service_name", serviceName),
		)

		// 在“流量发生器”这层也做一部分正则匹配，方便在 Profiles 中看到
		// traffic_generator_request 这个 span 的 CPU 占用。
		_ = checkEmail()

		// Make Request (调用慢接口 /slow，更直观地看到 span 级火焰图)
		start := time.Now()
		resp, err := client.Get("http://localhost:8080" + routeSlow)
		duration := time.Since(start).Seconds()

		// Extract trace context for correlation
		spanCtx := span.SpanContext()
		traceID := spanCtx.TraceID().String()
		spanID := spanCtx.SpanID().String()

		if err != nil {
			// Log Error
			r := otel_log.Record{}
			r.SetTimestamp(time.Now())
			r.SetSeverity(otel_log.SeverityError)
			r.SetSeverityText("ERROR")
			// 为了便于在 Loki 中直接通过关键字搜索，这里把关键信息都写入 body 文本
			r.SetBody(otel_log.StringValue(fmt.Sprintf(
				"[ERROR] route=%s method=GET err=%v trace_id=%s span_id=%s",
				routeSlow, err, traceID, spanID,
			)))
			r.AddAttributes(
				otel_log.String("route", routeSlow),
				otel_log.String("error", err.Error()),
				otel_log.String("trace_id", traceID),
				otel_log.String("span_id", spanID),
			)
			logger.Emit(iterCtx, r)
			span.RecordError(err)
		} else {
			requestCount.Add(iterCtx, 1, metric.WithAttributes(
				attribute.String("method", "GET"),
				attribute.String("status", "200"),
				attribute.String("route", routeSlow),
			))
			histogram.Record(iterCtx, duration, metric.WithAttributes(attribute.String("route", routeSlow)))
			resp.Body.Close()

			// Log Success
			r := otel_log.Record{}
			r.SetTimestamp(time.Now())
			r.SetSeverity(otel_log.SeverityInfo)
			r.SetSeverityText("INFO")
			// 在 body 中直接包含 route=/slow 和 trace / span 信息，方便在日志里全文检索
			r.SetBody(otel_log.StringValue(fmt.Sprintf(
				"[OK] route=%s method=GET status=200 duration_ms=%d trace_id=%s span_id=%s",
				routeSlow, int(duration*1000), traceID, spanID,
			)))
			r.AddAttributes(
				otel_log.String("route", routeSlow),
				otel_log.Int("duration_ms", int(duration*1000)),
				otel_log.String("method", "GET"),
				otel_log.String("trace_id", traceID),
				otel_log.String("span_id", spanID),
			)
			logger.Emit(iterCtx, r)
		}

		span.End()
		time.Sleep(time.Millisecond * time.Duration(100+rand.Intn(500)))
	}
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	// 业务逻辑（otelhttp 中间件已自动创建顶层 span 和 metrics）
	time.Sleep(time.Millisecond * time.Duration(rand.Intn(50)))

	// Randomly fail
	if rand.Float32() < 0.05 {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write([]byte("Hello World"))
}

// allocHandler: 模拟“内存分配很多”的接口，用来在 Profiles 或 Mem 图上观察内存占用变化。
// - 点击 /alloc 一次，会在当前进程中分配一批中等大小的字节切片，并缓存到全局变量中。
// - 为了避免真的 OOM，只在一个上限范围内循环复用这一批内存。
func allocHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := tracer.Start(ctx, "alloc_business_logic")
	defer span.End()

	allocateMemoryBurst()

	w.Write([]byte("Alloc endpoint finished"))
}

// slowHandler: 模拟一个“慢接口”
// - 会启动一个名为 slow_business_logic 的 span
// - 在 span 内做大量正则匹配（邮箱校验），CPU 使用率明显
// 在 Tempo 中点击这个 span 的 Profiles for this span，可以非常直观地看到火焰图。
func slowHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := tracer.Start(ctx, "slow_business_logic")
	defer span.End()

	// 模拟耗时的业务逻辑：多次调用高成本的邮箱校验逻辑
	for i := 0; i < 50; i++ {
		_ = checkEmail()
	}

	w.Write([]byte("Slow endpoint finished"))
}

// step 用来在 handler 内部创建一个子 span，并模拟少量 CPU 工作。
func step(ctx context.Context, name string) trace.Span {
	ctx, span := tracer.Start(ctx, name)
	// 模拟一点轻量级 CPU 计算，这里也用邮箱校验以保持一致
	_ = checkEmail()
	return span
}

// checkEmail 模拟“写得不合理的邮箱正则校验”，通过大量重复正则匹配制造 CPU 压力。
// - pattern: 一个较为复杂的邮箱正则
// - text:    一段刻意构造的“坏邮箱字符串”，在很多引擎下会触发灾难性回溯
//
// 在 Go 的 RE2 引擎中不会真的卡死，但重复执行足够多次，仍然能在火焰图中清晰看到
// 与正则相关的调用栈（如 regexp.* / runtime.*）。
func checkEmail() bool {
	matched := false
	// 重复匹配多次，放大单次匹配的开销
	for i := 0; i < 5000; i++ {
		if emailRegexp.MatchString(slowEmailSample) {
			matched = true
		}
	}
	return matched
}

// allocateMemoryBurst 模拟一次请求中分配一批内存，并保存在全局切片中。
// 这样在一段时间内可以观察到进程的 RSS / GC 行为，但不会无限制增长到 OOM。
func allocateMemoryBurst() {
	const (
		chunkSize   = 256 * 1024 // 每块 256KB
		chunkCount  = 200        // 每次请求分配 200 块，大约 50MB
		maxRetained = 20         // 最多保留 20 批（约 1GB 上限，实际会被 GC/操作系统回收一部分）
	)

	// 如果已经保留了很多批数据，就丢弃最早的一批，避免无限增长
	if len(allocHolder) >= maxRetained {
		allocHolder = allocHolder[1:]
	}

	batch := make([]byte, chunkSize*chunkCount)
	// 简单写入一点数据，避免被编译器优化掉
	for i := range batch {
		batch[i] = byte(i)
	}
	allocHolder = append(allocHolder, batch)
}
