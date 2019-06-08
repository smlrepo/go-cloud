module gocloud.dev/internal/cmd/gocdk

go 1.12

require (
	github.com/google/go-cmp v0.3.0
	github.com/shurcooL/httpfs v0.0.0-20190527155220-6a4d4a70508b // indirect
	github.com/shurcooL/vfsgen v0.0.0-20181202132449-6a9ea43bcacd
	github.com/spf13/cobra v0.0.4
	gocloud.dev v0.15.0
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	golang.org/x/sys v0.0.0-20190606203320-7fc4e5ec1444
	golang.org/x/tools v0.0.0-20190606174628-0139d5756a7d // indirect
	golang.org/x/xerrors v0.0.0-20190513163551-3ee3066db522
	google.golang.org/api v0.6.0
)

replace gocloud.dev => ../../../
