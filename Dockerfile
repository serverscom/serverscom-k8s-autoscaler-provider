FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY serverscom-k8s-autoscaler-provider /bin/serverscom-k8s-autoscaler-provider
ENTRYPOINT ["/bin/serverscom-k8s-autoscaler-provider"]
