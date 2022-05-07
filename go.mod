module cm-int-api-template

go 1.15

replace vendor.lib/tng/tng-lib => bitbucket.centene.com/tng/tng-lib v1.0.10

require (
	vendor.lib/tng/tng-lib v0.0.0-00010101000000-000000000000
	github.com/denisenkom/go-mssqldb v0.0.0-20200910202707-1e08a3fab204
	github.com/pkg/errors v0.9.1
	github.com/rs/cors v1.7.0
	github.com/rs/zerolog v1.20.0
)
