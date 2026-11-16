module hitall.com/middle-platform/sdk-core

go 1.27.3

require (
	github.com/go-sql-driver/mysql v1.10.1
	golang.org/x/net v0.62.0
	hitall.com/middle-platform/shared v0.0.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace hitall.com/middle-platform/shared => ../shared
