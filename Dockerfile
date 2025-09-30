FROM golang:1.25-alpine3.22 AS build

RUN apk add --no-cache make git

WORKDIR /app
COPY . .
RUN go mod download
RUN make docker-build

FROM alpine:3.21

WORKDIR /app
COPY --from=build /app/build/rabbit ./rabbit

EXPOSE 6969
ENTRYPOINT ["./rabbit"]
