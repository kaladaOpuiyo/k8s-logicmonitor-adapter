FROM golang:1.14 as build
WORKDIR /k8s-logicmonitor-adapter
ARG VERSION
COPY ./ ./
RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /adapter  cmd/adapter/main.go

FROM golang:1.14 as test
WORKDIR /k8s-logicmonitor-adapter
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v1.26.0
RUN ./bin/golangci-lint --version
COPY --from=build /k8s-logicmonitor-adapter ./
RUN chmod +x ./scripts/test.sh; sync; ./scripts/test.sh
RUN cp coverage.txt /coverage.txt

FROM alpine:3.6
RUN apk --update add ca-certificates \
    && rm -rf /var/cache/apk/* \
    && rm -rf /var/lib/apk/*
WORKDIR /app
COPY --from=test /coverage.txt /coverage.txt
COPY --from=build /adapter /bin

ENTRYPOINT ["adapter"]
