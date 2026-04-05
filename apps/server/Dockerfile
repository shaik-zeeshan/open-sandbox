FROM golang:1.25.5-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/open-sandbox .

FROM docker:28.5.2-cli

RUN apk add --no-cache ca-certificates curl docker-cli-compose

WORKDIR /app

COPY --from=build /out/open-sandbox /usr/local/bin/open-sandbox

EXPOSE 8080

CMD ["open-sandbox"]
