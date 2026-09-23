# ── 构建阶段 ──
FROM golang:1.23.9 AS builder

WORKDIR /app

# GOPROXY 可通过构建参数覆盖（例如国内环境：--build-arg GOPROXY=https://goproxy.cn,direct）
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# VERSION 通过构建参数注入二进制版本号（-ldflags），默认 dev
ARG VERSION=dev
# TARGETOS/TARGETARCH 在使用 buildx 构建多架构镜像时自动注入
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /app/inspector ./cmd/inspector

# ── 运行阶段 ──
FROM alpine:3.19.7

LABEL org.opencontainers.image.title="k8s-inspector" \
      org.opencontainers.image.description="Kubernetes cluster intelligent inspection tool" \
      org.opencontainers.image.source="https://github.com/ithina/k8s-inspector" \
      org.opencontainers.image.licenses="MIT"

ENV TZ=Asia/Shanghai \
    APP_HOME=/app \
    KUBE_HOME=/home/vmuser/.kube

RUN apk add --no-cache shadow tzdata ca-certificates && \
    ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    groupadd -r -g 700 vmuser && \
    useradd -r -u 700 -g vmuser -d /home/vmuser -s /sbin/nologin vmuser && \
    mkdir -p ${KUBE_HOME} ${APP_HOME} && \
    chown -R vmuser:vmuser /home/vmuser ${APP_HOME} && \
    chmod 750 ${APP_HOME}

WORKDIR ${APP_HOME}

# HTML 报告模板已通过 embed 内置到二进制，无需额外拷贝模板文件
COPY --from=builder --chown=vmuser:vmuser /app/inspector ${APP_HOME}/inspector

USER vmuser

ENTRYPOINT ["./inspector"]
CMD ["--once"]
