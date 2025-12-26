## LGTMP 栈演示环境（Loki + Grafana + Tempo + Mimir + Pyroscope）

这个项目是一个完整的可观测性演示环境，基于 Grafana 的 **LGTM + Pyroscope** 技术栈，并配套一个用 Go 编写的"合成压测应用"，持续产生日志、链路、指标和性能分析（Profiles），方便在本地演示 **Logs → Traces → Metrics → Profiles** 的联动能力。

### Java 应用接入（一句话总结）

**服务端已搭建完成，Java 应用只需：下载两个 JAR 包，启动时添加一个 `-javaagent` 参数，配置环境变量指向服务端即可。零代码侵入，自动实现 Traces → Profiles 关联。**

详细接入流程见下方"Java 应用接入"章节。

### 组件一览

- **Loki**：日志存储
- **Grafana**：可视化与探索（面板 / Explore / Trace / Flamegraph）
- **Tempo**：分布式追踪存储（支持 TraceQL、Service Graph、Span Metrics）
- **Mimir**：Prometheus 兼容时序数据库（用来存储 RED 指标和 Span Metrics）
- **Pyroscope**：持续性能分析（CPU / 内存等）
- **Grafana Alloy**：统一的 OTLP + Profiling 采集与转发 Agent

### 快速开始

#### ARM64 架构（Mac M1/M2/M3）

1. 启动整套环境：

   ```bash
   cd lgtmp-grafana-main
   docker-compose up -d --build
   ```

#### x86_64 架构（Linux/Windows/Intel Mac）

1. 启动整套环境：

   ```bash
   cd lgtmp-grafana-main
   docker-compose -f docker-compose-x86.yaml up -d --build
   ```

   **注意**：首次build可能需要5-10分钟（下载依赖、编译应用）

#### 通用步骤

2. 打开 Grafana：

   - 地址：`http://localhost:3000`
   - 默认已开启匿名登录（Admin）

3. 在 Grafana 中依次体验：

   - **Logs（Loki）**：查看 `job="demo-app"` 的日志；点击 `Tempo` 字段跳到对应 Trace。
   - **Traces（Tempo）**：在 span 详情中点击：
     - 文档图标 → 跳回对应日志（Traces → Logs，使用自定义 LogQL：`{job="${__span.tags.job}"} |= "${__span.traceId}"`）
     - 火焰图图标 → 跳到 Pyroscope，看该 span 对应的性能分析（Traces → Profiles）
     - Metrics 链接（可选开启）→ 跳到 Mimir 查看该 span 的 RED 指标

4. 停止环境：

   ```bash
   # ARM64
   docker-compose down

   # x86_64
   docker-compose -f docker-compose-x86.yaml down
   ```

### 架构概览

- **应用（`app/main.go`）**：
  - 通过 OTLP/HTTP 上报 Traces / Metrics / Logs 到 **Grafana Alloy**；
  - 通过 Pyroscope Go SDK 上报 CPU/内存 Profile 到 **Pyroscope**。
- **Alloy**：
  - 接收应用的 OTLP 数据；
  - 按类型转发到 Loki / Tempo / Mimir。
- **Tempo**：
  - 启用 `metrics_generator`，从 Trace 生成 Span Metrics / Service Graph，写入 Mimir。
- **Grafana**：
  - 通过 `configs/grafana-datasources.yaml` 预配置 Loki / Tempo / Mimir / Pyroscope 数据源及各种"Trace ↔ Logs / Metrics / Profiles / Node Graph"的跳转规则。

---

## 代码侵入性对比：Java vs Python vs Go

本项目提供了 **Java**、**Python** 和 **Go** 三种应用的可观测性接入演示，展示了不同语言实现 **Traces + Logs + Metrics** 的侵入性差异：

- **Java**：完全零代码侵入（JavaAgent 自动生成所有信号）
- **Python**：Traces/Logs 零侵入，Metrics 轻微侵入（约 30 行集中代码，业务代码无需修改）
- **Go**：完全侵入（每个 handler 都要手动埋点）

### Java 应用 - 零代码侵入 ✅

**特点**：使用 OpenTelemetry Java Agent 自动埋点，**无需修改任何源码**

**接入方式**：
```bash
java -javaagent:/path/to/opentelemetry-javaagent.jar \
     -Dotel.javaagent.extensions=/path/to/pyroscope-otel.jar \
     -jar your-app.jar
```

**优势**：
- ✅ 零代码侵入，现有应用无需修改
- ✅ 自动埋点（HTTP、数据库、框架等）
- ✅ 通过环境变量配置，灵活切换
- ✅ 自动关联 Traces → Profiles（通过 Pyroscope OTel Extension）

**实现原理**：
- OpenTelemetry Java Agent 在 JVM 启动时使用字节码增强技术
- 自动为 Spring Boot、Servlet、JDBC 等框架注入追踪代码
- Pyroscope Extension 在 span 上添加 `pyroscope.profile.id` 实现关联

### Python 应用 - 轻微侵入（Traces/Logs 零侵入，Metrics 需 30 行代码）✅

**特点**：使用 OpenTelemetry Python 自动埋点，Traces 和 Logs 零侵入，Metrics 通过 Flask 钩子实现

**接入方式**：
```bash
# 使用 opentelemetry-instrument 命令启动应用
opentelemetry-instrument python app.py
```

**优势**：
- ✅ Traces/Logs 零代码侵入，现有应用无需修改
- ✅ Metrics 轻微侵入（约 30 行集中代码，业务代码无需修改）
- ✅ 自动埋点（Flask、Django、FastAPI、requests等）
- ✅ 通过环境变量配置，灵活切换
- ✅ 支持多种Python框架和库
- ✅ 新增接口自动获得 Metrics，无需额外埋点

**实现原理**：
- **Traces/Logs**：OpenTelemetry Python 使用 monkey patching 技术自动埋点
- 自动为Flask、Django、FastAPI等框架注入追踪代码
- 自动拦截HTTP请求、数据库调用等
- **Metrics**：使用 Flask 钩子（`before_request`/`after_request`）自动记录所有 HTTP 请求
- 集中初始化 OpenTelemetry Metrics SDK，复用自动配置的 MeterProvider
- 业务代码无需任何修改，新增接口自动获得指标

**Demo 应用**：
- 本项目提供了 Python Flask 演示应用（`PythonDemo/`）
- 提供三个测试接口：`/hello`（正常）、`/slow`（CPU密集）、`/alloc`（内存密集）
- 使用 `opentelemetry-instrument` 零代码启动，Traces 和 Logs 完全无需修改源码
- 访问地址：`http://localhost:18082`（x86_64）

**Metrics 实现说明**：
- ✅ **已支持 HTTP Metrics 上报**：通过轻微代码侵入（约 30 行集中代码）实现
- 实现方式：
  - 使用 Flask `before_request`/`after_request` 钩子自动拦截所有 HTTP 请求
  - 业务代码（`/hello`、`/slow`、`/alloc`、`/health`）**完全无需修改**
  - 新增接口自动获得指标，无需额外埋点
- 指标类型：
  - `http_server_duration_milliseconds`：HTTP 请求耗时直方图
  - `http_server_request_count`：HTTP 请求计数器
  - 标签：`http.method`、`http.route`、`http.status_code`、`job`
- 侵入性对比：
  - **Go 方式**：每个 handler 都要埋点（N 个接口 × 10+ 行代码）
  - **Python 方式**：集中初始化，业务代码零修改（1 个文件 × 30 行代码）
- Python 应用当前支持：
  - ✅ Traces → Tempo（零代码侵入）
  - ✅ Logs → Loki（零代码侵入）
  - ✅ Metrics → Mimir（轻微侵入，业务代码无需修改）

### Go 应用 - SDK 集成（有侵入）📝

**特点**：需要在代码中集成 OpenTelemetry Go SDK

**接入方式**：
```go
import (
    "go.opentelemetry.io/otel"
    "github.com/grafana/pyroscope-go"
    "github.com/grafana/otel-profiling-go"
)

// 初始化 tracer, meter, logger
tracer := otel.Tracer("demo-app")

// 手动创建 span
ctx, span := tracer.Start(ctx, "operation_name")
defer span.End()
```

**优势**：
- ✅ 更细粒度的控制（自定义 span、attributes）
- ✅ 更好的性能（按需埋点）
- ✅ 适合复杂业务场景

**劣势**：
- ❌ 需要修改源码
- ❌ 需要开发人员理解 OpenTelemetry 概念
- ❌ 代码维护成本较高

### 对比总结

| 语言 | 代码侵入 | 接入方式 | 自动 Traces | 自动 Logs | 自动 Metrics | 适用场景 |
|------|----------|----------|------------|-----------|--------------|----------|
| **Java** | ✅ 零侵入 | -javaagent | ✅ | ✅ | ✅ | Spring Boot、企业应用 |
| **Python** | ⚠️ 轻微侵入 | opentelemetry-instrument + Flask钩子 | ✅ | ✅ | ✅ 需30行代码 | Flask、Django、FastAPI |
| **Go** | ❌ 需侵入 | SDK集成 | ✅ 手动 | ✅ 手动 | ✅ 手动 | 微服务、高性能应用 |

**注**：
- **Java**：完全零代码侵入，JavaAgent 自动生成所有信号
- **Python**：Traces/Logs 零侵入，Metrics 需约 30 行集中代码（业务代码无需修改）
- **Go**：所有信号都需要在每个 handler 中手动埋点

---

## Grafana 使用指南 - 如何查看可观测性数据

本项目已预配置好所有数据源，并实现了 **Logs → Traces → Metrics → Profiles** 的全链路关联。以下是详细的使用方法：

### 1️⃣ 查看日志（Loki）

**步骤**：
1. 打开 Grafana：`http://localhost:3000`
2. 点击左侧菜单 **Explore**（探索图标）
3. 顶部选择数据源：**Loki**
4. 在查询框输入：`{job="demo-app"}`
5. 点击 **Run query** 运行查询

**你会看到**：
- 应用的所有日志流
- 每条日志包含：时间戳、日志内容、trace_id、span_id
- 日志格式示例：
  ```
  [OK] route=/slow method=GET status=200 duration_ms=7262
  trace_id=e72aed33e504dd9f82d890f64dee516a span_id=b8f5e8166150350c
  ```

**高级功能**：
- **过滤日志**：`{job="demo-app"} |= "error"` （只看包含error的日志）
- **JSON解析**：`{job="demo-app"} | json | route="/slow"` （解析JSON字段）
- **跳转到Trace**：点击日志右侧的 **Tempo** 按钮，直接跳转到对应的追踪详情

### 2️⃣ 查看追踪（Tempo）

**步骤**：
1. 在 Grafana Explore 中，选择数据源：**Tempo**
2. 有三种查询方式：

**方式A：通过TraceQL搜索**
```
{ service.name="demo-app" }
```

**方式B：通过Service选择**
- 点击 **Search** 标签
- Service Name 选择：`demo-app`
- 点击 **Run query**

**方式C：从日志跳转**
- 在Loki日志中，点击日志右侧的 **Tempo** 按钮
- 自动跳转到对应的trace

**你会看到**：
- Trace列表：每个HTTP请求的完整调用链路
- 点击任意trace，查看详细的瀑布图（Waterfall）
- Span详情：每个操作的耗时、状态、标签

**Trace详情页功能**：
- **Span详情**：点击任意span查看详细信息
- **跳转到Logs**：点击span右侧的 **📄 文档图标** → 查看该span对应的日志
- **跳转到Profiles**：点击span右侧的 **🔥 火焰图图标** → 查看CPU/内存分析
- **Node Graph**：点击 **Node Graph** 标签 → 查看服务调用关系图

### 3️⃣ 查看指标（Mimir/Prometheus）

**步骤**：
1. 在 Grafana Explore 中，选择数据源：**Mimir**
2. 点击 **Metrics browser** 选择指标，或直接输入PromQL

**常用查询示例**：

```promql
# 查询请求速率（QPS）
rate(http_server_request_duration_seconds_count{job="demo-app"}[5m])

# 查询请求延迟（P95）
histogram_quantile(0.95, rate(http_server_request_duration_seconds_bucket{job="demo-app"}[5m]))

# 查询错误率
rate(http_server_request_duration_seconds_count{job="demo-app",status=~"5.."}[5m])
```

**你会看到**：
- 时序图表：指标随时间变化的趋势
- 可切换图表类型：线图、柱状图、表格等

**从Traces跳转到Metrics**：
- 在Tempo的span详情中，点击 **Metrics** 链接
- 自动跳转到该span相关的RED指标（Rate、Error、Duration）

### 4️⃣ 查看性能分析（Pyroscope）

**步骤**：
1. 在 Grafana Explore 中，选择数据源：**Pyroscope**
2. 选择应用：`demo-app`
3. 选择Profile类型：
   - **CPU**：查看CPU热点
   - **Alloc Space**：查看内存分配
   - **Inuse Space**：查看内存占用

**你会看到**：
- **火焰图（Flamegraph）**：可视化展示函数调用栈和耗时占比
- **热点函数**：火焰图中越宽的部分，表示该函数占用资源越多
- **调用路径**：从下往上是函数调用链

**火焰图阅读技巧**：
- **宽度**：函数占用资源的比例（越宽=越热）
- **高度**：调用栈的深度
- **颜色**：无特殊含义，仅用于区分不同函数
- **交互**：
  - 点击函数块：放大该函数的子调用
  - 双击空白：重置视图
  - 鼠标悬停：查看函数详细信息

**从Traces跳转到Profiles**：
- 在Tempo的span详情中，点击 **🔥 火焰图图标**
- 自动定位到该span执行期间的性能分析数据
- **这是最强大的功能**：精确定位某个请求慢在哪个函数上！

### 5️⃣ 全链路追踪演示案例

#### 案例：排查 `/slow` 接口慢的原因

**步骤1：从日志发现问题**
1. Explore → Loki
2. 查询：`{job="demo-app"} | json | route="/slow"`
3. 发现该接口耗时 6000-8000ms

**步骤2：查看追踪详情**
1. 点击日志右侧的 **Tempo** 按钮
2. 进入Trace详情页
3. 看到 `slow_business_logic` span 占用了大部分时间

**步骤3：查看性能分析定位代码**
1. 点击 `slow_business_logic` span
2. 点击右侧的 **🔥 火焰图图标**
3. 跳转到Pyroscope
4. 火焰图显示：`checkEmail` 函数占用99% CPU
5. 继续展开：`regexp.MatchString` 是热点
6. **结论**：正则表达式性能问题！

**步骤4：验证修复效果**
1. 修复代码后重新部署
2. 在Mimir中对比修复前后的P95延迟
3. 在Pyroscope中确认CPU热点消失

### 6️⃣ 常见问题排查

| 问题 | 排查工具 | 查询方式 |
|------|----------|----------|
| 接口慢 | Tempo + Pyroscope | Trace瀑布图 + 火焰图 |
| 接口报错 | Loki + Tempo | 日志搜索 + Trace详情 |
| 内存泄漏 | Pyroscope | Alloc Space火焰图 |
| CPU高 | Pyroscope | CPU火焰图 |
| 服务依赖 | Tempo | Node Graph / Service Graph |
| 接口超时 | Tempo + Mimir | Trace + P99延迟指标 |

### 7️⃣ 快捷键和技巧

**Grafana通用快捷键**：
- `Ctrl/Cmd + K`：打开命令面板
- `Ctrl/Cmd + O`：跳转到Dashboard
- `Esc`：关闭模态窗口

**Explore页面技巧**：
- **Split视图**：点击右上角 **Split** 按钮，并排对比两个数据源
- **查询历史**：点击查询框下方的 **History** 查看历史查询
- **共享查询**：点击右上角 **Share** 生成链接分享给团队

---

## 演示接口与故障场景

演示应用暴露了三个典型接口，用来分别演示：

1. **正常快速请求（基线）**
2. **正则表达式导致的 CPU 热点**
3. **单次请求大量分配内存导致的内存压力**

所有接口的实现都在 `app/main.go` 中。

### `/hello` —— 正常快速请求

处理函数：`helloHandler`

- 逻辑：只做极少量工作，返回 `"Hello World"`。
- 作用：作为基线，对比"正常延迟 / 正常 CPU" 的样子。
- 在 Grafana 中：
  - **Trace**：span 持续时间非常短。
  - **Logs**：`route="/hello"`，`duration_ms` 很小。
  - **Profiles**：对 `/hello` 对应 span 点 "Profiles for this span"，几乎看不到明显的 CPU 热点。

### `/slow` —— 正则校验导致的 CPU 高占用（`checkEmail`）

处理函数：`slowHandler`，内部有业务 span：`slow_business_logic`。

- 逻辑：
  - 在 `slow_business_logic` span 中，多次调用 `checkEmail`。
  - `checkEmail` 使用了一条复杂的邮箱正则，对一段刻意构造的"坏邮箱字符串"做高频匹配：

    ```go
    // 模拟写得不合理的邮箱正则校验，通过大量重复正则匹配制造 CPU 压力
    emailRegexp = regexp.MustCompile(`^(\w+([-.][A-Za-z0-9]+)*){3,18}@\w+([-.][A-Za-z0-9]+)*\.\w+([-.][A-Za-z0-9]+)*$`)
    slowEmailSample = "rosamariachoccelahuaaranda70@gmail.comnnbbb.bbNG.bbb.n¿.?n"

    func checkEmail() bool {
        matched := false
        for i := 0; i < 5000; i++ {
            if emailRegexp.MatchString(slowEmailSample) {
                matched = true
            }
        }
        return matched
    }
    ```

- 触发方式：

  ```bash
  curl http://localhost:8080/slow
  ```

  实际上，应用内部自带的"流量发生器"也会持续请求 `/slow`，不手动调也会有数据。

- 如何在图里看到异常：
  - **Logs（Loki）**：搜索 `route=/slow`，可以看到形如
    `[OK] route=/slow method=GET status=200 duration_ms=... trace_id=... span_id=...` 的日志。
  - **Traces（Tempo）**：打开任意一条 Trace，找到 `slow_business_logic` span。
  - **Traces → Profiles（Tempo → Pyroscope）**：
    点击火焰图按钮，能看到一条非常"笔直"的栈：
    `… → main.slowHandler → main.checkEmail → regexp.(*Regexp).MatchString / doMatch / backtrack ...`，
    很容易向团队解释"是某个正则写得太复杂 / 输入太极端，把 CPU 烧满了"。

### `/alloc` —— 模拟内存占用 / 泄漏倾向

处理函数：`allocHandler`，对应业务 span：`alloc_business_logic`。

- 逻辑：
  - 每次请求调用 `allocateMemoryBurst`，分配一大块内存并缓存到全局变量 `allocHolder` 中：

    ```go
    func allocateMemoryBurst() {
        const (
            chunkSize   = 256 * 1024 // 每块 256KB
            chunkCount  = 200        // 每次请求分配约 50MB
            maxRetained = 20         // 最多保留 20 批（约 1GB 上限，避免真正 OOM）
        )

        if len(allocHolder) >= maxRetained {
            allocHolder = allocHolder[1:]
        }

        batch := make([]byte, chunkSize*chunkCount)
        for i := range batch {
            batch[i] = byte(i)
        }
        allocHolder = append(allocHolder, batch)
    }
    ```

- 触发方式：

  ```bash
  # 多打几次，模拟"一个接口每次请求都偷偷吃一大块内存"
  for i in {1..20}; do curl -s http://localhost:8080/alloc > /dev/null; done
  ```

- 如何在图里看到异常：
  - **Metrics / 进程监控**：`demo-app` 的内存占用（RSS）会阶梯式上升，然后在 `maxRetained` 附近趋于平稳。
  - **Traces（Tempo）**：`alloc_business_logic` span 会比普通请求明显更"肥"（耗时更长）。
  - **Profiles（Pyroscope）**：对该 span 点 "Profiles for this span"，可以看到热点集中在
    `make([]byte)` / `runtime.mallocgc` / `memmove` 等内存分配相关函数上。

结合 `/hello`、`/slow`、`/alloc` 三个案例，你可以在一次演示中向团队展示：

1. **正常请求**：链路短、CPU 和内存都很轻；
2. **CPU 异常**：通过 Trace + Flamegraph 一眼看出是 `checkEmail` 这样的业务逻辑在烧 CPU；
3. **内存异常**：通过 Trace + Profile + Metrics 看出是哪个接口在"悄悄吃内存"，并找到具体代码位置（`allocateMemoryBurst` 一类函数）。

---

## Java 应用接入（零代码侵入方案）

### 核心结论

**服务端（LGTMP 栈）已经搭建完成，Java 应用接入只需要：**

1. **下载两个 JAR 包**：
   - `opentelemetry-javaagent.jar`（OpenTelemetry Java Agent）
   - `pyroscope-otel.jar`（Pyroscope OTel Extension）

2. **启动时添加一个 `-javaagent` 参数**：
   ```bash
   java -javaagent:/path/to/opentelemetry-javaagent.jar -jar your-app.jar
   ```

3. **配置环境变量**（指向已搭建好的 LGTMP 栈）：
   ```bash
   OTEL_EXPORTER_OTLP_ENDPOINT=http://alloy:4318
   OTEL_JAVAAGENT_EXTENSIONS=/path/to/pyroscope-otel.jar
   OTEL_PYROSCOPE_SERVER_ADDRESS=http://pyroscope:4040
   ```

**就这么简单！无需修改 Java 应用代码，无需改造服务端。**

---

### 完整接入流程

#### 前提条件

✅ **LGTMP 栈已启动**（`docker-compose up -d`）：
- Alloy（采集层）：`http://alloy:4318`
- Tempo（链路追踪）：`http://tempo:3200`
- Loki（日志）：`http://loki:3100`
- Mimir（指标）：`http://mimir:9009`
- Pyroscope（性能分析）：`http://pyroscope:4040`

#### 步骤 1：下载两个 JAR 文件

```bash
# 下载 OpenTelemetry Java Agent（自动埋点 Traces/Metrics/Logs）
wget https://github.com/open-telemetry/opentelemetry-java-instrumentation/releases/download/v1.39.0/opentelemetry-javaagent.jar

# 下载 Pyroscope OTel Extension（自动关联 Traces 和 Profiles）
wget https://github.com/grafana/otel-profiling-java/releases/download/v0.5.1/pyroscope-otel.jar
```

#### 步骤 2：启动 Java 应用（添加 JVM 参数和环境变量）

**方式 A：命令行启动**

```bash
java \
  -javaagent:/path/to/opentelemetry-javaagent.jar \
  -Dotel.service.name=java-demo-app \
  -Dotel.exporter.otlp.endpoint=http://alloy:4318 \
  -Dotel.exporter.otlp.protocol=http/protobuf \
  -Dotel.javaagent.extensions=/path/to/pyroscope-otel.jar \
  -Dotel.pyroscope.application.name=java-demo-app \
  -Dotel.pyroscope.server.address=http://pyroscope:4040 \
  -Dotel.pyroscope.start.profiling=true \
  -jar your-app.jar
```

**方式 B：Docker 启动（推荐）**

在 `docker-compose.yaml` 中添加：

```yaml
java-app:
  image: openjdk:8-jre-slim
  environment:
    - OTEL_SERVICE_NAME=java-demo-app
    - OTEL_EXPORTER_OTLP_ENDPOINT=http://alloy:4318
    - OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
    - OTEL_JAVAAGENT_EXTENSIONS=/app/pyroscope-otel.jar
    - OTEL_PYROSCOPE_APPLICATION_NAME=java-demo-app
    - OTEL_PYROSCOPE_SERVER_ADDRESS=http://pyroscope:4040
    - OTEL_PYROSCOPE_START_PROFILING=true
  volumes:
    - ./opentelemetry-javaagent.jar:/app/opentelemetry-javaagent.jar
    - ./pyroscope-otel.jar:/app/pyroscope-otel.jar
    - ./your-app.jar:/app/your-app.jar
  command: java -javaagent:/app/opentelemetry-javaagent.jar -jar /app/your-app.jar
  networks:
    - lgtmp-net
```

#### 步骤 3：验证接入

1. **访问应用**：确认应用正常运行
2. **在 Grafana 中验证**：
   - **Tempo**：查看 `service.name=java-demo-app` 的 Traces
   - **Pyroscope**：查看 `java-demo-app` 的火焰图
   - **Traces → Profiles**：在 Tempo 的 span 详情中点击"Profiles for this span"，应该能看到对应的火焰图

---

### 工作原理（简要说明）

1. **`-javaagent:opentelemetry-javaagent.jar`**：
   - 启动 OpenTelemetry Java Agent
   - 自动为 Spring Boot、HTTP、数据库等框架埋点
   - 通过 `OTEL_EXPORTER_OTLP_ENDPOINT` 发送到 Alloy

2. **`OTEL_JAVAAGENT_EXTENSIONS=/path/to/pyroscope-otel.jar`**：
   - OpenTelemetry Agent 自动加载 Pyroscope Extension
   - Extension 在 span 上添加 `pyroscope.profile.id` 标签
   - 同时将相同 ID 注入到 Pyroscope profile 样本中
   - 实现 Traces 和 Profiles 的精确关联

3. **数据流**：
   ```
   Java 应用
     ↓ -javaagent:opentelemetry-javaagent.jar
   OpenTelemetry Agent（自动埋点）
     ↓ OTLP/HTTP → Alloy → Tempo/Loki/Mimir

   Pyroscope Extension（通过环境变量加载）
     ↓ 带 pyroscope.profile.id 的 Profile → Pyroscope
   ```

---

### 总结

✅ **服务端已搭建完成**：LGTMP 栈无需任何改造
✅ **Java 应用只需**：
   - 下载两个 JAR 包
   - 启动时添加一个 `-javaagent` 参数
   - 配置环境变量指向服务端
✅ **零代码侵入**：无需修改 Java 应用代码
✅ **自动关联**：Traces → Profiles 跳转与 Go 应用效果完全一致
