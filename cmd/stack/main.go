package main

import (
	"bytes"
	"fmt"
)

type cmd struct {
	Depth int
	line  int

	desc   bytes.Buffer
	inner  int
	idx    int
	start  string
	starts []string
	sep    string
	seps   []string
}

func main() {
	fmt.Println("ok")
	c := &cmd{}

	c.Line("pwd")
	cl := c.StartList("", "|")
	c.Line("cat foo.list")
	c.Line("awk '{print $1}'")
	c.Line("cat > out.filter")
	cl("")
	c.Line("env")
	fmt.Println(c.desc.String())
}

func prefix(d int) string {
	v := ""
	for i := 0; i < d; i++ {
		v = v + "  "
	}
	return v
}
func (d *cmd) Line(cmd string) {
	if d.inner > 0 {
		d.idx++
		if d.idx > 1 {
			d.desc.WriteString(d.seps[d.inner-1] + cmd)
			return
		}
		//if d.inner == 1 {
		//	d.desc.WriteString(fmt.Sprintf("%2d: %s", d.line, prefix(d.Depth)))
		//}
		// d.desc.WriteString(fmt.Sprintf("%s%s", d.start, cmd))
		d.desc.WriteString(fmt.Sprintf("%s%s", d.starts[d.inner-1], cmd))
		return
	}
	d.line++
	d.desc.WriteString(fmt.Sprintf("%2d: %s%s\n", d.line, prefix(d.Depth), cmd))
}

func (d *cmd) StartList(start, sep string) (endlist func(end string)) {
	if d.inner == 0 {
		d.line++
		d.idx = 0
		d.desc.WriteString(fmt.Sprintf("%2d: %s", d.line, prefix(d.Depth)))
	}
	d.sep = sep // WILL BREAK.
	d.seps = append(d.seps, sep)
	d.starts = append(d.starts, start)
	//if d.start != "" {
	//	d.start = d.start + " " + start
	//} else {
	//	d.start = start
	//}
	d.inner++
	endlist = func(end string) {
		//d.start = "" // WILL BREAK.
		d.starts = d.starts[:len(d.starts)-1]
		d.seps = d.seps[:len(d.seps)-1]

		// Verify that some commands were printed before adding extra newline.
		if d.idx > 0 {
			d.desc.WriteString(end)
			if d.inner == 1 {
				d.desc.WriteString("\n")
			}
		}
		d.inner--
	}
	return endlist
}
