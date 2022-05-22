package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/russross/blackfriday/v2"
)

// siteDir is the target directory into which the HTML gets generated. Its
// default is set here but can be changed by an argument passed into the
// program.
var siteDir = "./public"

func verbose() bool {
	return len(os.Getenv("VERBOSE")) > 0
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func ensureDir(dir string) {
	err := os.MkdirAll(dir, 0755)
	check(err)
}

func copyFile(src, dst string) {
	dat, err := os.ReadFile(src)
	check(err)
	err = os.WriteFile(dst, dat, 0644)
	check(err)
}

func mustReadFile(path string) string {
	bytes, err := os.ReadFile(path)
	check(err)
	return string(bytes)
}

func markdown(src string) string {
	return string(blackfriday.Run([]byte(src)))
}

func readLines(path string) []string {
	src := mustReadFile(path)
	return strings.Split(src, "\n")
}

func mustGlob(glob string) []string {
	paths, err := filepath.Glob(glob)
	check(err)
	return paths
}

func whichLexer(path string) string {
	if strings.HasSuffix(path, ".go") {
		return "go"
	} else if strings.HasSuffix(path, ".sh") {
		return "console"
	} else {
		return "hash"
	}
	//panic("No lexer for " + path)
}

func debug(msg string) {
	if os.Getenv("DEBUG") == "1" {
		_, err := fmt.Fprintln(os.Stderr, msg)
		if err != nil {
			// 向 Stderr 写入内容基本不可能失败，这里只是为了抑制 IDE 的提示。
			return
		}
	}
}

// 短破折号 —— "-"
var dashPat = regexp.MustCompile(`-+`)

// Seg is a segment of an example
type Seg struct {
	Docs, DocsRendered              string
	Code, CodeRendered, CodeForJs   string
	CodeEmpty, CodeLeading, CodeRun bool
}

// Example is info extracted from an example file
type Example struct {
	ID, Name    string
	RealName    string // 渲染首页列表时需要的"中文"名称
	Html        string // 内容等于 Segs 中 DocsRendered 字段的集合；否则在渲染模版时就会出现"分段"并产生重复的 HTML 元素。
	GoCode      string
	GoCodeHash  string
	URLHash     string
	Segs        [][]*Seg
	PrevExample *Example
	NextExample *Example
}

func parseSegs(sourcePath string) ([]*Seg, string) {
	var (
		lines  []string
		source []string
		segs   []*Seg
	)
	// 将 tab 转换为4个空格以统一风格
	for _, line := range readLines(sourcePath) {
		lines = append(lines, strings.Replace(line, "\t", "    ", -1))
		source = append(source, line)
	}
	// 重新将"行"组合为"全文"。读取"行"的目的主要在于格式化。
	fileContent := strings.Join(source, "\n")

	lastSeen := "" // 上一个处理的"行"是什么：空行、注释还是代码
	for _, line := range lines {
		if line == "" {
			// 如果是"空行"就跳过
			lastSeen = ""
			continue
		}
		// 如果匹配到了"Go 注释"，那就说明不是"Go 代码"；所以这里对 matchDocs 取反
		newDocs := (lastSeen == "") || ((lastSeen != "docs") && (segs[len(segs)-1].Docs != ""))
		if newDocs {
			debug("NEWSEG")
		}

		if newDocs {
			newSeg := Seg{Docs: line, Code: ""}
			segs = append(segs, &newSeg)
		} else {
			segs[len(segs)-1].Docs = segs[len(segs)-1].Docs + "\n" + line
		}

		debug("DOCS: " + line)
		lastSeen = "docs"
	}
	for _, seg := range segs {
		seg.CodeEmpty = true
		seg.CodeLeading = false
		seg.CodeRun = false
	}
	return segs, fileContent
}

func parseAndRenderSegs(sourcePath string) ([]*Seg, string) {
	segs, fileContent := parseSegs(sourcePath)

	// 根据文件名的后缀决定使用什么语法分析器：go 或 shell script；
	// 但是目前仅能作为检查文件的后缀名了
	whichLexer(sourcePath)

	for _, seg := range segs {
		if seg.Docs != "" {
			seg.DocsRendered = markdown(seg.Docs)
		}
	}

	return segs, fileContent
}

// splitExampleName 文件 examples.txt 中的每一行都是以"英文文件名｜中文"的格式保存，这是为了在渲染首页时能灵活显示标题。
// 本函数就是将这种格式的名字分割为 list 以便获取"中文"部分。
func splitExampleName(name string) []string {
	splitNames := strings.Split(name, "|")
	if len(splitNames) == 1 {
		splitNames = append(splitNames, splitNames[0])
	}

	return splitNames
}

func parseExamples() []*Example {
	var exampleNames []string

	for _, line := range readLines("examples.txt") {
		if line != "" && !strings.HasPrefix(line, "#") {
			exampleNames = append(exampleNames, line)
		}
	}

	examples := make([]*Example, 0)
	for i, exampleName := range exampleNames {
		if verbose() {
			fmt.Printf("Processing %s [%d/%d]\n", exampleName, i+1, len(exampleNames))
		}
		splitNames := splitExampleName(exampleName)
		example := Example{
			Name:     splitNames[0],
			RealName: splitNames[1],
		}
		exampleID := strings.ToLower(splitNames[0])
		exampleID = strings.Replace(exampleID, " ", "-", -1)
		exampleID = strings.Replace(exampleID, "/", "-", -1)
		exampleID = strings.Replace(exampleID, "'", "", -1)
		exampleID = dashPat.ReplaceAllString(exampleID, "-")
		example.ID = exampleID
		example.Segs = make([][]*Seg, 0)
		sourcePaths := mustGlob("examples/" + exampleID + "/*.md")
		for _, sourcePath := range sourcePaths {
			sourceSegs, fileContents := parseAndRenderSegs(sourcePath)
			if fileContents != "" {
				example.GoCode = fileContents
			}
			example.Segs = append(example.Segs, sourceSegs)
			example.Html = markdown(fileContents)
		}

		examples = append(examples, &example)
	}

	// 生成前后链接
	for i, example := range examples {
		if i > 0 {
			example.PrevExample = examples[i-1]
		}
		if i < (len(examples) - 1) {
			example.NextExample = examples[i+1]
		}
	}
	return examples
}

func renderIndex(examples []*Example) {
	if verbose() {
		fmt.Println("Rendering index")
	}
	indexTmpl := template.New("index")
	_, err := indexTmpl.Parse(mustReadFile("templates/footer.tmpl"))
	check(err)
	_, err = indexTmpl.Parse(mustReadFile("templates/index.tmpl"))
	check(err)
	indexF, err := os.Create(siteDir + "/index.html")
	check(err)
	err = indexTmpl.Execute(indexF, examples)
	check(err)
}

func renderExamples(examples []*Example) {
	if verbose() {
		fmt.Println("Rendering examples")
	}
	exampleTmpl := template.New("example")
	_, err := exampleTmpl.Parse(mustReadFile("templates/footer.tmpl"))
	check(err)
	_, err = exampleTmpl.Parse(mustReadFile("templates/example.tmpl"))
	check(err)
	for _, example := range examples {
		exampleF, err := os.Create(siteDir + "/" + example.ID + ".html")
		check(err)
		err = exampleTmpl.Execute(exampleF, example)
		check(err)
	}
}

func main() {
	if len(os.Args) > 1 {
		siteDir = os.Args[1]
	}
	ensureDir(siteDir)

	copyFile("templates/site.css", siteDir+"/site.css")
	copyFile("templates/site.js", siteDir+"/site.js")
	copyFile("templates/favicon.ico", siteDir+"/favicon.ico")
	copyFile("templates/404.html", siteDir+"/404.html")
	copyFile("templates/play.png", siteDir+"/play.png")
	copyFile("templates/clipboard.png", siteDir+"/clipboard.png")
	examples := parseExamples()
	renderIndex(examples)
	renderExamples(examples)
}
