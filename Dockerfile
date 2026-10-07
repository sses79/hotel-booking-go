FROM golang:1.25.0-alpine3.22 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/hotel-api ./cmd/local \
 && CGO_ENABLED=0 go build -trimpath -o /out/hotel-schema ./cmd/schema \
 && CGO_ENABLED=0 go build -trimpath -o /out/hotel-outbox ./cmd/outbox \
 && CGO_ENABLED=0 go build -trimpath -o /out/hotel-eventbridge ./cmd/eventbridge

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/hotel-api /usr/local/bin/hotel-api
COPY --from=build /out/hotel-schema /usr/local/bin/hotel-schema
COPY --from=build /out/hotel-outbox /usr/local/bin/hotel-outbox
COPY --from=build /out/hotel-eventbridge /usr/local/bin/hotel-eventbridge
COPY schema ./schema
USER app
ENV PORT=8080 FUNCTION_TARGET=HotelAPI
EXPOSE 8080
CMD ["hotel-api"]
