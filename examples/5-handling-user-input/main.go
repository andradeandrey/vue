package main

import (
	"github.com/norunners/vue"
)

const tmpl = `
<p>{{ Message }}</p>
<button v-on:click="ReverseMessage">
  Reverse Message
</button>
`

type Data struct {
	Message string
}

func ReverseMessage(context vue.Context) {
	data := context.Data().(*Data)
	runes := []rune(data.Message)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	data.Message = string(runes)
}

func main() {
	vue.New(
		vue.El("#app"),
		vue.Template(tmpl),
		vue.Data(&Data{Message: "Hello WebAssembly!"}),
		vue.Methods(ReverseMessage),
	)

	select {}
}
