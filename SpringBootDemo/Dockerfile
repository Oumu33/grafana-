# 第一阶段：使用 Maven 构建应用
# 使用本地 Maven 包和 Agent JAR 文件（使用 arm64 8u342-jdk，多架构官方镜像）
FROM openjdk:8u342-jdk AS builder

WORKDIR /build

# 复制本地 Maven 包并解压（无需下载）
COPY apache-maven-3.8.8-bin.tar.gz .
RUN tar -xzf apache-maven-3.8.8-bin.tar.gz -C /opt && \
    ln -s /opt/apache-maven-3.8.8 /opt/maven && \
    rm apache-maven-3.8.8-bin.tar.gz

ENV PATH="/opt/maven/bin:${PATH}"

# 配置 Maven 使用阿里云镜像源（加速依赖下载）
RUN mkdir -p /root/.m2 && \
    echo '<?xml version="1.0" encoding="UTF-8"?>' > /root/.m2/settings.xml && \
    echo '<settings xmlns="http://maven.apache.org/SETTINGS/1.0.0"' >> /root/.m2/settings.xml && \
    echo '          xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"' >> /root/.m2/settings.xml && \
    echo '          xsi:schemaLocation="http://maven.apache.org/SETTINGS/1.0.0 http://maven.apache.org/xsd/settings-1.0.0.xsd">' >> /root/.m2/settings.xml && \
    echo '  <mirrors>' >> /root/.m2/settings.xml && \
    echo '    <mirror>' >> /root/.m2/settings.xml && \
    echo '      <id>aliyunmaven</id>' >> /root/.m2/settings.xml && \
    echo '      <mirrorOf>*</mirrorOf>' >> /root/.m2/settings.xml && \
    echo '      <name>阿里云公共仓库</name>' >> /root/.m2/settings.xml && \
    echo '      <url>https://maven.aliyun.com/repository/public</url>' >> /root/.m2/settings.xml && \
    echo '    </mirror>' >> /root/.m2/settings.xml && \
    echo '  </mirrors>' >> /root/.m2/settings.xml && \
    echo '</settings>' >> /root/.m2/settings.xml

# 先复制 pom.xml，单独做一层依赖缓存（后续改源码不需要重新下载依赖）
COPY pom.xml .
RUN mvn -DskipTests dependency:go-offline

# 再复制源代码与本地 Agent 文件
COPY src ./src
COPY opentelemetry-javaagent.jar .
COPY pyroscope-otel.jar .

# 构建应用（会复用上面 go-offline 下载的依赖，速度明显更快）
RUN mvn -DskipTests clean package

# 第二阶段：运行环境（与构建阶段同一基础镜像，arm64 8u342-jdk）
FROM openjdk:8u342-jdk

WORKDIR /app

# 从构建阶段复制应用 JAR 和 Agent 文件
COPY --from=builder /build/target/spring-boot-demo-0.0.1-SNAPSHOT.jar app.jar
COPY --from=builder /build/opentelemetry-javaagent.jar opentelemetry-javaagent.jar
COPY --from=builder /build/pyroscope-otel.jar pyroscope-otel.jar

# 使用 Java Agent 启动（零代码侵入）
# 1. -javaagent:/app/opentelemetry-javaagent.jar：启动 OpenTelemetry Agent（自动埋点 Traces/Metrics/Logs）
# 2. OTEL_JAVAAGENT_EXTENSIONS 环境变量：告诉 OpenTelemetry Agent 加载 Pyroscope Extension
#    Pyroscope Extension 会自动关联 Traces 和 Profiles（添加 pyroscope.profile.id 标签）
# 注意：只需要一个 -javaagent 参数，Pyroscope Extension 通过环境变量自动加载
ENTRYPOINT ["java", \
  "-Djava.security.egd=file:/dev/./urandom", \
  "-javaagent:/app/opentelemetry-javaagent.jar", \
  "-jar", "/app/app.jar"]
