# ── 构建阶段 ──
FROM harbor.xx/ic/golang:1.23.9 AS builder
WORKDIR /app
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
COPY . .
WORKDIR /app/cmd/inspector
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=$(git rev-parse --short HEAD 2>/dev/null || echo dev)" \
    -o /app/inspector .

# ── 运行阶段 ──
FROM harbor.xx/ic/alpine:3.19.7

LABEL org.opencontainers.image.title="k8s-inspector" \
      org.opencontainers.image.description="Kubernetes cluster intelligent inspection tool" \
      org.opencontainers.image.source="http://git.xx/k8s/k8s-inspector" \
      org.opencontainers.image.licenses="MIT"

ENV TZ=Asia/Shanghai \
    APP_HOME=/app \
    KUBE_HOME=/home/vmuser/.kube

RUN apk add --no-cache shadow curl tzdata ca-certificates && \
    ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    groupadd -r -g 700 vmuser && \
    useradd -r -u 700 -g vmuser -d /home/vmuser -s /sbin/nologin vmuser && \
    mkdir -p ${KUBE_HOME} ${APP_HOME} && \
    chown -R vmuser:vmuser /home/vmuser ${APP_HOME} && \
    chmod 750 ${APP_HOME}

WORKDIR ${APP_HOME}

COPY --from=builder --chown=vmuser:vmuser ${APP_HOME}/inspector ${APP_HOME}/
COPY --from=builder --chown=vmuser:vmuser ${APP_HOME}/templates ${APP_HOME}/templates

USER vmuser

ENTRYPOINT ["./inspector", "--once"]