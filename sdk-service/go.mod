module github.com/hctty27/middle-platform/sdk-service

go 1.27.3

require (
	github.com/hctty27/middle-platform/shared/bootstrap v0.0.0
	github.com/hctty27/middle-platform/shared/logging v0.0.0
	github.com/hctty27/middle-platform/shared/signature v0.0.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/hctty27/middle-platform/shared/logging => ../shared/logging

replace github.com/hctty27/middle-platform/shared/signature => ../shared/signature

replace github.com/hctty27/middle-platform/shared/bootstrap => ../shared/bootstrap
