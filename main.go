package main

import (
	"bufio"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/nitishm/engarde/pkg/parser"
	"istio.io/pkg/log"
)

const (
	Http       int = 0
	Tcp            = 1
	Udp            = 2
	Note           = 3
	Desc           = 4
	HttpAndTcp     = 5
	TcpAndUdp      = 6
)

type Entry struct {
	Name        string
	Description string
	Http        string
	Tcp         string
	Udp         string
	Note        string
	HttpAndTcp  string
	TcpAndUdp   string
}

type ResponseFlag struct {
	Name        string
	Description string
}

var (
	entries       = map[string]*Entry{}
	indent        = 0
	responseFlags = []ResponseFlag{}
)

type ViewerPageData struct {
	AccessLog  string
	ParesedLog *parser.AccessLog
}

type docInfo struct {
	Entry
	Header        string
	ResponseFlags []ResponseFlag
}

var pageData ViewerPageData

func docs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	ids, present := query["id"]
	if !present || len(ids) == 0 || ids[0] == "" {
		fmt.Println("id not present")
		w.WriteHeader(503)
		return
	}
	id := ids[0]
	key, header := parseID(id)

	docValue, exists := entries[key]
	if !exists {
		fmt.Printf("unknown log format token: %v\n", key)
		docValue = &Entry{}
	}

	d := docInfo{
		Entry:         *docValue,
		Header:        header,
		ResponseFlags: responseFlags,
	}
	var tmpl *template.Template
	if id == "response_flags" {
		tmpl = template.Must(template.ParseFiles("templates/response_flags.html"))
	} else {
		tmpl = template.Must(template.ParseFiles("templates/envoy_doc.html"))
	}
	if err := tmpl.Execute(w, d); err != nil {
		fmt.Println(err)
	}
}

func update(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if len(r.Form["message"]) != 1 {
		mainPage(w, r)
		return
	}
	rawLog := parseInput(r.Form["message"][0])
	if rawLog == "" {
		mainPage(w, r)
		return
	}

	useIstio := r.FormValue("use_istio") == "on"
	pageData.AccessLog = rawLog
	pageData.ParesedLog = parseAccessLog(fmt.Sprintf("%s", rawLog), useIstio)
	tmpl := template.Must(template.ParseFiles("templates/main.html"))
	if err := tmpl.Execute(w, pageData); err != nil {
		fmt.Println(err)
	}
}

func mainPage(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/main.html"))
	pageData = ViewerPageData{
		AccessLog:  "",
		ParesedLog: nil,
	}
	tmpl.Execute(w, pageData)
}

func main() {

	parseEnvoyDocs()

	http.HandleFunc("/", mainPage)
	http.HandleFunc("/update", update)
	http.HandleFunc("/docs", docs)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	fmt.Println("starting server on port 9090 . . . ")

	http.ListenAndServe(":9090", nil)
}

func parseInput(input string) (retval string) {
	retval = strings.TrimSpace(input)
	retval = strings.ReplaceAll(retval, "\n", "")
	retval = strings.ReplaceAll(retval, "\r", "")

	return retval
}

func parseEnvoyDocs() {
	var client http.Client
	resp, err := client.Get("https://www.envoyproxy.io/docs/envoy/latest/_sources/configuration/observability/access_log/usage.rst.txt")
	if err != nil {
		log.Fatal(err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		scanner := bufio.NewScanner(resp.Body)
		firstItem := false
		var currEntry *Entry
		init := false
		state := -1
		for scanner.Scan() {
			text := scanner.Text()
			if strings.Index(text, "%") == 0 {
				if !firstItem {
					firstItem = true
				}
				if currEntry != nil {
					entries[currEntry.Name] = currEntry
				}
				currEntry = &Entry{
					Name: text,
				}
			} else if firstItem {
				trimmed := strings.Trim(text, " ")
				if !init {
					indent = len(scanner.Text()) - len(trimmed)
					init = true
				}

				if trimmed == "" {
					continue
				}

				if trimmed == "HTTP" {
					state = Http
					scanner.Scan()
					trimmed = strings.Trim(scanner.Text(), " ")
				} else if trimmed == "TCP" {
					state = Tcp
					scanner.Scan()
					trimmed = strings.Trim(scanner.Text(), " ")
				} else if trimmed == "UDP" {
					state = Udp
					scanner.Scan()
					trimmed = strings.Trim(scanner.Text(), " ")
				} else if trimmed == "TCP/UDP" {
					state = TcpAndUdp
					scanner.Scan()
					trimmed = strings.Trim(scanner.Text(), " ")
				} else if trimmed == "HTTP/TCP" || trimmed == "HTTP and TCP" {
					state = HttpAndTcp
					scanner.Scan()
					trimmed = strings.Trim(scanner.Text(), " ")
				} else if trimmed == ".. note::" {
					state = Note
					scanner.Scan()
					trimmed = strings.Trim(scanner.Text(), " ")
				} else if strings.HasPrefix(trimmed, ".. _") {
					continue
				} else if ((len(scanner.Text()) - len(trimmed)) / indent) == 1 {
					state = Desc
				}
				if currEntry.Name == "%RESPONSE_FLAGS%" {
					addResponseFlag(trimmed)
				}
				updateEntry(trimmed, state, currEntry)
			}
		}
	}
	for _, entry := range entries {
		entry.Description = fixDocLinks(entry.Description)
		entry.Http = fixDocLinks(entry.Http)
		entry.Tcp = fixDocLinks(entry.Tcp)
		entry.HttpAndTcp = fixDocLinks(entry.HttpAndTcp)
		entry.TcpAndUdp = fixDocLinks(entry.TcpAndUdp)
		entry.Udp = fixDocLinks(entry.Udp)
		entry.Note = fixDocLinks(entry.Note)
	}
	for i := range responseFlags {
		responseFlags[i].Description = fixDocLinks(responseFlags[i].Description)
	}
}

func updateEntry(descr string, state int, e *Entry) {
	switch state {
	case Http:
		e.Http = updateDescription(e.Http, descr)
	case Tcp:
		e.Tcp = updateDescription(e.Tcp, descr)
	case Udp:
		e.Udp = updateDescription(e.Udp, descr)
	case TcpAndUdp:
		e.TcpAndUdp = updateDescription(e.TcpAndUdp, descr)
	case HttpAndTcp:
		e.HttpAndTcp = updateDescription(e.HttpAndTcp, descr)
	case Note:
		e.Note = updateDescription(e.Note, descr)
	case Desc:
		e.Description = updateDescription(e.Description, descr)
	}
}

func updateDescription(current, new string) string {
	if current == "" {
		return new
	}
	return fmt.Sprintf("%v\n%v", current, new)
}

func addResponseFlag(flag string) {
	if strings.HasPrefix(flag, "* **") {
		elements := strings.Split(flag, "**")
		name := elements[1]
		description := elements[2]
		responseFlags = append(responseFlags, ResponseFlag{Name: name, Description: description})
	}
}

func parseAccessLog(text string, istio bool) *parser.AccessLog {
	var p *parser.Parser

	if istio {
		p = parser.New(parser.IstioProxyAccessLogsPattern)
	} else {
		p = parser.New(parser.EnvoyAccessLogsPattern)
	}

	accessLog, err := p.Parse(text)
	if err != nil {
		accessLog.ParseError = err.Error()
	}

	return accessLog
}

func parseID(id string) (key, header string) {

	switch id {
	case "authority":
		key = "%REQ(X?Y):Z%"
		header = "%REQ(:AUTHORITY)%"
	case "bytes_received":
		key = "%BYTES_RECEIVED%"
	case "bytes_sent":
		key = "%BYTES_SENT%"
	case "duration":
		key = "%DURATION%"
	case "forwarded_for":
		key = "%REQ(X?Y):Z%"
		header = "%REQ(FORWARDED-FOR)%"
	case "method":
		key = "%REQ(X?Y):Z%"
		header = "%REQ(:METHOD)%"
	case "protocol":
		key = "%PROTOCOL%"
	case "request_id":
		key = "%REQ(X?Y):Z%"
		header = "%REQ(X-REQUEST-ID)%"
	case "response_flags":
		key = "%RESPONSE_FLAGS%"
	case "status_code":
		key = "%RESPONSE_CODE%"
	case "tcp_service_time":
		key = ""
	case "timestamp":
		key = "%START_TIME%"
	case "upstream_service":
		key = "%UPSTREAM_HOST%"
	case "upstream_service_time":
		key = "%RESP(X?Y):Z%"
		header = "%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%"
	case "upstream_cluster":
		key = "%UPSTREAM_CLUSTER%"
	case "upstream_local":
		key = "%UPSTREAM_LOCAL_ADDRESS%"
	case "downstream_local":
		key = "%DOWNSTREAM_LOCAL_ADDRESS%"
	case "downstream_remote":
		key = "%DOWNSTREAM_REMOTE_ADDRESS%"
	case "requested_server":
		key = "%REQUESTED_SERVER_NAME%"
	case "uri_param":
		key = ""
	case "uri_path":
		key = "%REQ(X?Y):Z%"
		header = "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"
	case "user_agent":
		key = "%REQ(X?Y):Z%"
		header = "%REQ(USER-AGENT)%"
	case "response_details":
		key = "%RESPONSE_CODE_DETAILS%"
	case "route_name":
		key = "%ROUTE_NAME%"
	case "termination_details":
		key = "%CONNECTION_TERMINATION_DETAILS%"
	}
	return key, header
}

func fixDocLinks(rawText string) string {
	retval := rawText
	for strings.Contains(retval, ":ref:") {
		retval = removeDocLink(retval)
	}
	return retval
}

func removeDocLink(ref string) string {
	start := strings.Index(ref, ":ref:`")
	retval := strings.Replace(ref, ":ref:`", "", 1)
	left, right := 0, 0
	for i := start; i < len(retval); i++ {
		c := retval[i : i+1]
		if c == "<" {
			left = i
		} else if c == ">" {
			right = i + 1
			break
		}
	}
	return retval[:left] + retval[right+1:]
}
