module github.com/gnolang/gno/contribs/valdoctor

go 1.24.0

toolchain go1.24.4

replace github.com/gnolang/gno => ../..

require (
	github.com/gnolang/gno v0.0.0-00010101000000-000000000000
	github.com/pelletier/go-toml v1.9.5
	github.com/stretchr/testify v1.11.1
	golang.org/x/term v0.40.0
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/peterbourgon/ff/v3 v3.4.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	golang.org/x/sys v0.41.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
