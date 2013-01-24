package xgo

import (
	"errors"
	"net/http"
	// "net/url"
	"reflect"
	"regexp"
	"strings"
)

type xgoResponseWriter struct {
	writer    http.ResponseWriter
	HasOutput bool
}

func (this *xgoResponseWriter) Header() http.Header {
	return this.writer.Header()
}

func (this *xgoResponseWriter) Write(p []byte) (int, error) {
	this.HasOutput = true
	return this.writer.Write(p)
}

func (this *xgoResponseWriter) WriteHeader(code int) {
	this.HasOutput = true
	this.writer.WriteHeader(code)
}

type xgoRoutingRule struct {
	Pattern        string
	Regexp         *regexp.Regexp
	Params         []string
	ControllerType reflect.Type
}

type xgoRouter struct {
	app         *xgoApp
	Rules       []*xgoRoutingRule
	StaticRules []*xgoRoutingRule
	StaticDir   map[string]string
}

func (this *xgoRouter) SetStaticPath(sPath, fPath string) {
	this.StaticDir[sPath] = fPath
}

func (this *xgoRouter) AddRule(pattern string, c xgoControllerInterface) error {
	rule := &xgoRoutingRule{
		Pattern:        "",
		Regexp:         nil,
		Params:         []string{},
		ControllerType: reflect.Indirect(reflect.ValueOf(c)).Type(),
	}
	paramCnt := strings.Count(pattern, ":")
	if paramCnt > 0 {
		re, err := regexp.Compile(`:\w+\(.*?\)`)
		if err != nil {
			return err
		}
		matches := re.FindAllStringSubmatch(pattern, paramCnt)
		if len(matches) != paramCnt {
			return errors.New("Regexp match error")
		}
		for _, match := range matches {
			m := match[0]
			index := strings.Index(m, "(")
			rule.Params = append(rule.Params, m[0:index])
			pattern = "^" + strings.Replace(pattern, m, m[index:], 1)
		}
		re, err = regexp.Compile(pattern)
		if err != nil {
			return err
		}
		rule.Regexp = re
		this.Rules = append(this.Rules, rule)
	} else {
		rule.Pattern = pattern
		this.StaticRules = append(this.StaticRules, rule)
	}
	return nil
}

func (this *xgoRouter) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	// defer func() {
	// if err := recover(); err != nil {
	// fmt.Println("RECOVER:", err)
	// if !RecoverPanic {
	// 	panic(err)
	// } else {
	// 	Critical("Handler crashed with error", err)
	// 	for i := 1; ; i += 1 {
	// 		_, file, line, ok := runtime.Caller(i)
	// 		if !ok {
	// 			break
	// 		}
	// 		Critical(file, line)
	// 	}
	// }
	// }
	// }()

	w := &xgoResponseWriter{
		writer:    rw,
		HasOutput: false,
	}
	var routingRule *xgoRoutingRule
	urlPath := r.URL.Path
	pathLen := len(urlPath)
	pathEnd := urlPath[pathLen-1]

	//static file server
	if r.Method == "GET" || r.Method == "HEAD" {
		for sPath, fPath := range this.StaticDir {
			if strings.HasPrefix(urlPath, sPath) {
				file := fPath + urlPath[len(sPath):]
				http.ServeFile(w, r, file)
				return
			}
		}
	}

	//first find path from the fixrouters to Improve Performance
	for _, rule := range this.StaticRules {
		if urlPath == rule.Pattern || (pathEnd == '/' && urlPath[:pathLen-1] == rule.Pattern) {
			routingRule = rule
			break
		}
	}

	if routingRule == nil {
		for _, rule := range this.Rules {
			if !rule.Regexp.MatchString(urlPath) {
				continue
			}
			matches := rule.Regexp.FindStringSubmatch(urlPath)[1:]
			paramCnt := len(rule.Params)
			if paramCnt != len(matches) {
				continue
			}
			if paramCnt > 0 {
				values := r.URL.Query()
				for i, match := range matches {
					values.Add(rule.Params[i], match)
				}
				r.URL.RawQuery = values.Encode()
			}
			routingRule = rule
			break
		}
	}

	if routingRule == nil {
		http.NotFound(w, r)
		return
	}

	r.ParseForm()
	ci := reflect.New(routingRule.ControllerType).Interface()
	ctx := &xgoContext{
		Response: w,
		Request:  r,
	}
	tpl := &xgoTemplate{
		tpl:       nil,
		tplVars:   make(map[string]interface{}),
		tplResult: nil,
	}
	sess := &xgoSession{
		sessionManager: this.app.session,
		sessionId:      ctx.GetCookie(SessionName),
		ctx:            ctx,
		data:           nil,
	}
	util.CallMethod(ci, "Init", ctx, tpl, sess, routingRule.ControllerType.Name())
	if w.HasOutput {
		return
	}

	hc := &HookController{
		Context:  ctx,
		Template: tpl,
		Session:  sess,
	}

	this.app.hook.CallControllerHook("AfterInit", urlPath, hc)
	if w.HasOutput {
		return
	}

	var method string
	switch r.Method {
	case "GET":
		method = "Get"
	case "POST":
		method = "Post"
	case "HEAD":
		method = "Head"
	case "DELETE":
		method = "Delete"
	case "PUT":
		method = "Put"
	case "PATCH":
		method = "Patch"
	case "OPTIONS":
		method = "Options"
	default:
		http.Error(w, "Method Not Allowed", 405)
	}

	this.app.hook.CallControllerHook("Before"+r.Method, urlPath, hc)
	if w.HasOutput {
		return
	}

	util.CallMethod(ci, method)
	if w.HasOutput {
		return
	}

	this.app.hook.CallControllerHook("After"+r.Method, urlPath, hc)
	if w.HasOutput {
		return
	}

	this.app.hook.CallControllerHook("BeforeRender", urlPath, hc)
	if w.HasOutput {
		return
	}

	util.CallMethod(ci, "Render")
	if w.HasOutput {
		return
	}

	this.app.hook.CallControllerHook("AfterRender", urlPath, hc)
	if w.HasOutput {
		return
	}

	this.app.hook.CallControllerHook("BeforeOutput", urlPath, hc)
	if w.HasOutput {
		return
	}

	util.CallMethod(ci, "Output")
	if w.HasOutput {
		return
	}

	this.app.hook.CallControllerHook("AfterOutput", urlPath, hc)
	if w.HasOutput {
		return
	}
}
