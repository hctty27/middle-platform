module github.com/hctty27/middle-platform/config-center

go 1.27.3

require (
	github.com/go-chi/chi/v5 v5.5.0
	github.com/go-sql-driver/mysql v1.10.1
	github.com/hctty27/middle-platform/shared/logging v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require filippo.io/edwards25519 v1.2.0 // indirect

replace github.com/hctty27/middle-platform/shared/logging => ../shared/logging
