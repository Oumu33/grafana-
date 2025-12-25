# SpringBootDemo

Spring Boot 示例应用，已配置零代码侵入接入 LGTMP 栈（Traces、Metrics、Logs、Profiles）。

## 快速开始

### 直接启动（多阶段构建，无需本地 Maven）

在 `lgtmp-demo` 根目录下：

```bash
docker-compose up -d --build java-app
```

**说明**：
- Dockerfile 使用多阶段构建：
  - 第一阶段：使用 Maven 镜像自动构建应用（无需本地安装 Maven）
  - 第二阶段：使用 JRE 镜像运行应用，并自动下载 OpenTelemetry Agent 和 Pyroscope Extension
- 所有依赖和工具都在 Docker 构建过程中自动处理，无需手动操作

### 4. 验证

- **访问应用**：`http://localhost:18081/`（会 sleep 3 秒，模拟慢接口）
- **Grafana**：
  - **Tempo**：查看 `service.name=java-demo-app` 的 Traces
  - **Pyroscope**：查看 `java-demo-app` 的火焰图
  - **Traces → Profiles**：在 Tempo 的 span 详情中点击“Profiles for this span”，应该能看到对应的火焰图

## API 接口

1. `GET /`：慢接口（`Thread.sleep(3000)`），模拟“慢请求/慢 IO”，主要用于看 Trace 的耗时
2. `GET /?name=cheney`：带参数的慢接口
3. `GET /random`：快速接口，返回随机字符串示例
4. `GET /slow`：**CPU 慢接口（正则回溯热点）**，用于在 Pyroscope 火焰图里直观看到 `checkEmail` 这样的热点函数
   - 可选参数：`loops`（默认 5000），例如：`/slow?loops=20000`
5. `GET /cpu`：**按时长烧 CPU（更容易出“尖刺”）**
   - 可选参数：`ms`（默认 3000），例如：`/cpu?ms=15000`
5. `GET /alloc`：**内存压力接口**，用于观察内存分配/持有带来的变化
   - 可选参数：
     - `mb`：本次分配大小（默认 50），例如：`/alloc?mb=100`
     - `hold`：是否持有引用（默认 true，false 更像“短时分配压力”）
     - `clear`：是否清空历史持有（默认 false），例如：`/alloc?clear=true`

## 如何制造明显的尖刺（推荐压测方式）

> Profiles/指标大多是采样或按时间窗口聚合的：**单次请求很难在图上形成“尖刺”**，需要“持续一段时间 + 并发”。

### CPU 尖刺

```bash
# 连续 30 秒、并发 8 个线程，按时长烧 CPU
hey -z 30s -c 8 "http://localhost:18081/cpu?ms=3000"
```

### 内存尖刺（或持续抬升）

```bash
# 先清空历史持有
curl "http://localhost:18081/alloc?clear=true"

# 连续持有内存（每次 50MB，最多持有到 256MB 上限）
for i in {1..10}; do curl -s "http://localhost:18081/alloc?mb=50&hold=true" >/dev/null; done
```

## 零代码侵入原理

- **OpenTelemetry Java Agent**：通过 `-javaagent` 参数自动为 Spring Boot、HTTP、数据库等框架埋点
- **Pyroscope OTel Extension**：作为 OTel Agent 的扩展，自动在 span 上添加 `pyroscope.profile.id`，实现 Traces 和 Profiles 的精确关联
- **无需修改代码**：所有配置通过环境变量和 JVM 参数完成

## 配置说明

所有配置在 `docker-compose.yaml` 的 `java-app` 服务中：

- `OTEL_EXPORTER_OTLP_ENDPOINT`：指向 Alloy（`http://alloy:4318`）
- `OTEL_SERVICE_NAME`：服务名称（`java-demo-app`）
- `OTEL_JAVAAGENT_EXTENSIONS`：加载 Pyroscope Extension
- `OTEL_PYROSCOPE_SERVER_ADDRESS`：Pyroscope 服务地址

详细信息请参考 `lgtmp-demo/README.md` 中的“Java 应用接入”章节。
