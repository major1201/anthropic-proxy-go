FROM alpine:3.21

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.ustc.edu.cn/g' /etc/apk/repositories && apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY bin/proxy .
COPY config/ ./config/

RUN mkdir -p /app/logs

EXPOSE 8000

CMD ["./proxy", "--config", "config/settings.json"]
