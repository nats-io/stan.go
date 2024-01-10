# syntax=docker/dockerfile:1.4
#See https://aka.ms/containerfastmode to understand how Visual Studio uses this Dockerfile to build your images for faster debugging.

FROM golang:1.16.15-alpine3.15 AS base
WORKDIR /src
COPY . .
WORKDIR /src/examples/stan-bench
RUN go build
ENTRYPOINT ["./stan-bench"]