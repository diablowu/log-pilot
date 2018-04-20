package pilot

import (
	"testing"
	"text/template"
	"os"
)

func TestRenderFunc(t *testing.T) {

	tpl, err := template.New("test-yml").Funcs(fm).Parse(TPL_BASE + "\n" + TPL_KAFKA)
	if err != nil {
		t.Fatal(err)
	}

	ctx := make(map[string]string, 0)

	tpl.Execute(os.Stdout, ctx)
}
