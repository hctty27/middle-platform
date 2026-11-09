module hitall.com/middle-platform/config-center

go 1.27.3

require (
	github.com/go-chi/chi/v5 v5.5.0
	github.com/go-sql-driver/mysql v1.10.1
	hitall.com/middle-platform/shared v0.0.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace hitall.com/middle-platform/shared => ../shared
