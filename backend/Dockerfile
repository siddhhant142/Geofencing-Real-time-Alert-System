FROM golang:1.21-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /geofencing-api .

FROM alpine:3.19
WORKDIR /app
COPY --from=build /geofencing-api /app/geofencing-api
EXPOSE 8080
CMD ["/app/geofencing-api"]
