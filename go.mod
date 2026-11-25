module github.com/standards-lab/docinfer

go 1.27.4

require (
	github.com/standards-lab/docproc v0.4.2
	github.com/standards-lab/go-ai v0.4.0
)

require golang.org/x/text v0.44.0 // indirect

replace github.com/standards-lab/go-ai => ../go-ai
