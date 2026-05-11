FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN go build -o /out/mvp-api ./cmd/server

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/mvp-api /app/mvp-api
ENV PORT=8080
EXPOSE 8080
CMD ["/app/mvp-api"]
