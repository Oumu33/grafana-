## LGTMP 栈演示环境（Loki + Grafana + Tempo + Mimir + Pyroscope）

这个项目是一个完整的可观测性演示环境，基于 Grafana 的 **LGTM + Pyroscope** 技术栈，并配套一个用 Go 编写的“合成压测应用”，持续产生日志、链路、指标和性能分析（Profiles），方便在本地演示 **Logs → Traces → Metrics → Profiles** 的联动能力。

### Java 应用接入（一句话总结）

**服务端已搭建完成，Java 应用只需：下载两个 JAR 包，启动时添加一个 `-javaagent` 参数，配置环境变量指向服务端即可。零代码侵入，自动实现 Traces → Profiles 关联。**

详细接入流程见下方“Java 应用接入”章节。

### 组件一览

- **Loki**：日志存储
- **Grafana**：可视化与探索（面板 / Explore / Trace / Flamegraph）
- **Tempo**：分布式追踪存储（支持 TraceQL、Service Graph、Span Metrics）
- **Mimir**：Prometheus 兼容时序数据库（用来存储 RED 指标和 Span Metrics）
- **Pyroscope**：持续性能分析（CPU / 内存等）
- **Grafana Alloy**：统一的 OTLP + Profiling 采集与转发 Agent

### 快速开始

1. 启动整套环境：

   ```bash
   cd lgtmp-demo
   docker-compose up -d --build
   ```

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
   docker-compose down
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
  - 通过 `configs/grafana-datasources.yaml` 预配置 Loki / Tempo / Mimir / Pyroscope 数据源及各种“Trace ↔ Logs / Metrics / Profiles / Node Graph”的跳转规则。

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
- 作用：作为基线，对比“正常延迟 / 正常 CPU” 的样子。
- 在 Grafana 中：
  - **Trace**：span 持续时间非常短。
  - **Logs**：`route="/hello"`，`duration_ms` 很小。
  - **Profiles**：对 `/hello` 对应 span 点 “Profiles for this span”，几乎看不到明显的 CPU 热点。

### `/slow` —— 正则校验导致的 CPU 高占用（`checkEmail`）

处理函数：`slowHandler`，内部有业务 span：`slow_business_logic`。

- 逻辑：
  - 在 `slow_business_logic` span 中，多次调用 `checkEmail`。
  - `checkEmail` 使用了一条复杂的邮箱正则，对一段刻意构造的“坏邮箱字符串”做高频匹配：

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

  实际上，应用内部自带的“流量发生器”也会持续请求 `/slow`，不手动调也会有数据。

- 如何在图里看到异常：
  - **Logs（Loki）**：搜索 `route=/slow`，可以看到形如  
    `[OK] route=/slow method=GET status=200 duration_ms=... trace_id=... span_id=...` 的日志。
  - **Traces（Tempo）**：打开任意一条 Trace，找到 `slow_business_logic` span。
  - **Traces → Profiles（Tempo → Pyroscope）**：  
    点击火焰图按钮，能看到一条非常“笔直”的栈：  
    `… → main.slowHandler → main.checkEmail → regexp.(*Regexp).MatchString / doMatch / backtrack ...`，  
    很容易向团队解释“是某个正则写得太复杂 / 输入太极端，把 CPU 烧满了”。

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
  # 多打几次，模拟“一个接口每次请求都偷偷吃一大块内存”
  for i in {1..20}; do curl -s http://localhost:8080/alloc > /dev/null; done
  ```

- 如何在图里看到异常：
  - **Metrics / 进程监控**：`demo-app` 的内存占用（RSS）会阶梯式上升，然后在 `maxRetained` 附近趋于平稳。
  - **Traces（Tempo）**：`alloc_business_logic` span 会比普通请求明显更“肥”（耗时更长）。
  - **Profiles（Pyroscope）**：对该 span 点 “Profiles for this span”，可以看到热点集中在  
    `make([]byte)` / `runtime.mallocgc` / `memmove` 等内存分配相关函数上。

结合 `/hello`、`/slow`、`/alloc` 三个案例，你可以在一次演示中向团队展示：

1. **正常请求**：链路短、CPU 和内存都很轻；
2. **CPU 异常**：通过 Trace + Flamegraph 一眼看出是 `checkEmail` 这样的业务逻辑在烧 CPU；
3. **内存异常**：通过 Trace + Profile + Metrics 看出是哪个接口在“悄悄吃内存”，并找到具体代码位置（`allocateMemoryBurst` 一类函数）。

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
   - **Traces → Profiles**：在 Tempo 的 span 详情中点击“Profiles for this span”，应该能看到对应的火焰图

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
