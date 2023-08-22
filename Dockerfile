FROM golang:1.16 AS build

WORKDIR /app

COPY . .

RUN go build -o speedtest-go

FROM alpine:latest

COPY --from=build /app/speedtest-go /usr/local/bin/speedtest-go

CMD ["go", "run", "api.go"]