module github.com/standards-lab/docinfer/cmd/docinfer

go 1.27.4

require (
	github.com/standards-lab/docinfer v0.2.0
	github.com/standards-lab/docproc v0.9.0
	github.com/standards-lab/go-ai v0.4.0
	github.com/standards-lab/go-cli-sdk v0.4.0
	github.com/standards-lab/go-core v0.12.0
)

require golang.org/x/text v0.44.0 // indirect

replace github.com/standards-lab/docinfer => ../..

replace github.com/standards-lab/go-ai => ../../../go-ai
