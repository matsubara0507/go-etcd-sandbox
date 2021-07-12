# go-etcd-sandbox

## vendor

```sh
go mod verify
go mod tidy -v
go mod vendor
find vendor -name BUILD.bazel -delete
bazel run //:gazelle
```
