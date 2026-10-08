package discovery

import (
	"encoding/json"
	"fmt"
	"net"
	"path"
	"sort"
	"strings"
)

type directive struct {
	name     string
	args     []string
	children []directive
}

func tokenizeConfig(data string) ([]string, error) {
	tokens := []string{}
	var word strings.Builder
	quote := rune(0)
	escaped, comment := false, false
	flush := func() {
		if word.Len() > 0 {
			tokens = append(tokens, word.String())
			word.Reset()
		}
	}
	for _, r := range data {
		if comment {
			if r == '\n' {
				comment = false
			}
			continue
		}
		if escaped {
			word.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == '#' {
			flush()
			comment = true
			continue
		}
		if r == '{' || r == '}' || r == ';' {
			flush()
			tokens = append(tokens, string(r))
			continue
		}
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			flush()
		} else {
			word.WriteRune(r)
		}
	}
	flush()
	if quote != 0 || escaped {
		return nil, fmt.Errorf("unterminated configuration token")
	}
	return tokens, nil
}

func parseDirectives(tokens []string, position *int, nested bool) ([]directive, error) {
	out := []directive{}
	for *position < len(tokens) {
		if tokens[*position] == "}" {
			*position++
			if !nested {
				return nil, fmt.Errorf("unexpected closing block")
			}
			return out, nil
		}
		d := directive{name: tokens[*position]}
		*position++
		terminated := false
		for *position < len(tokens) {
			token := tokens[*position]
			*position++
			switch token {
			case ";":
				terminated = true
			case "{":
				children, err := parseDirectives(tokens, position, true)
				if err != nil {
					return nil, err
				}
				d.children = children
				terminated = true
			case "}":
				return nil, fmt.Errorf("directive has no terminator")
			default:
				d.args = append(d.args, token)
			}
			if terminated {
				break
			}
		}
		if !terminated {
			return nil, fmt.Errorf("incomplete directive")
		}
		out = append(out, d)
	}
	if nested {
		return nil, fmt.Errorf("unclosed configuration block")
	}
	return out, nil
}

func ParseNginx(data []byte, source, proxy string, networks []string) ([]Route, error) {
	files := map[string]string{}
	order := []string{}
	current := "main"
	var content strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "# configuration file ") {
			if content.Len() > 0 {
				files[current] = content.String()
				order = append(order, current)
				content.Reset()
			}
			current = strings.TrimSuffix(strings.TrimPrefix(line, "# configuration file "), ":")
		} else {
			content.WriteString(line + "\n")
		}
	}
	if content.Len() > 0 {
		files[current] = content.String()
		order = append(order, current)
	}
	if len(order) == 0 {
		return []Route{}, nil
	}
	parsed := map[string][]directive{}
	for name, text := range files {
		tokens, err := tokenizeConfig(text)
		if err != nil {
			return nil, err
		}
		position := 0
		items, err := parseDirectives(tokens, &position, false)
		if err != nil {
			return nil, err
		}
		parsed[name] = items
	}
	var expand func([]directive, string, map[string]bool) ([]directive, error)
	expand = func(items []directive, file string, stack map[string]bool) ([]directive, error) {
		out := []directive{}
		for _, d := range items {
			if d.name == "include" && len(d.args) > 0 {
				pattern := d.args[0]
				if !strings.HasPrefix(pattern, "/") {
					pattern = path.Join(path.Dir(order[0]), pattern)
				}
				names := []string{}
				for name := range parsed {
					if match, _ := path.Match(pattern, name); match {
						names = append(names, name)
					}
				}
				sort.Strings(names)
				if len(names) == 0 && !strings.ContainsAny(pattern, "*?[") {
					return nil, fmt.Errorf("included configuration not present in dump")
				}
				for _, name := range names {
					if stack[name] {
						return nil, fmt.Errorf("configuration include cycle")
					}
					stack[name] = true
					children, err := expand(parsed[name], name, stack)
					delete(stack, name)
					if err != nil {
						return nil, err
					}
					out = append(out, children...)
				}
				continue
			}
			if d.children != nil {
				children, err := expand(d.children, file, stack)
				if err != nil {
					return nil, err
				}
				d.children = children
			}
			out = append(out, d)
		}
		return out, nil
	}
	root, err := expand(parsed[order[0]], order[0], map[string]bool{order[0]: true})
	if err != nil {
		return nil, err
	}
	upstreams := map[string][]string{}
	servers := []directive{}
	var walk func([]directive)
	walk = func(items []directive) {
		for _, d := range items {
			if d.name == "stream" {
				continue
			}
			if d.name == "upstream" && len(d.args) > 0 {
				for _, child := range d.children {
					if child.name == "server" && len(child.args) > 0 {
						upstreams[d.args[0]] = append(upstreams[d.args[0]], child.args[0])
					}
				}
			}
			if d.name == "server" && d.children != nil {
				servers = append(servers, d)
			} else {
				walk(d.children)
			}
		}
	}
	walk(root)
	routes := []Route{}
	for _, server := range servers {
		hosts := []string{}
		listens := [][]string{}
		rootPath := ""
		for _, d := range server.children {
			switch d.name {
			case "server_name":
				hosts = append(hosts, d.args...)
			case "listen":
				listens = append(listens, d.args)
			case "root":
				if len(d.args) > 0 {
					rootPath = d.args[0]
				}
			}
		}
		if len(hosts) == 0 {
			hosts = []string{"*"}
		}
		if len(listens) == 0 {
			listens = [][]string{{"80"}}
		}
		for _, listen := range listens {
			port := "80"
			scheme := "http"
			if len(listen) > 0 {
				if _, p, e := net.SplitHostPort(listen[0]); e == nil {
					port = p
				} else if !strings.Contains(listen[0], ":") {
					port = listen[0]
				}
			}
			for _, arg := range listen {
				if arg == "ssl" {
					scheme = "https"
				}
			}
			var emit func([]directive, string, []string)
			emit = func(items []directive, inheritedRoot string, paths []string) {
				kind, target, redirect := "static", "", ""
				ownRoot := inheritedRoot
				for _, d := range items {
					if d.name == "root" || d.name == "alias" {
						if len(d.args) > 0 {
							ownRoot = d.args[0]
						}
					}
					if (d.name == "proxy_pass" || d.name == "fastcgi_pass" || d.name == "uwsgi_pass" || d.name == "grpc_pass") && len(d.args) > 0 {
						kind, target = "proxy", d.args[0]
					}
					if d.name == "return" && len(d.args) > 1 {
						kind, redirect = "redirect", d.args[1]
					}
				}
				ups := []string{}
				if target != "" {
					key := target
					for _, prefix := range []string{"http://", "https://", "grpc://", "grpcs://"} {
						key = strings.TrimPrefix(key, prefix)
					}
					key = strings.TrimSuffix(key, "/")
					if named, ok := upstreams[key]; ok {
						ups = named
					} else {
						ups = []string{safeAddress(target)}
					}
				}
				r := Route{SourceID: source, ProxyID: proxy, Hosts: hosts, Paths: paths, Kind: kind, Scheme: scheme, Port: port, Root: ownRoot, Upstreams: ups, RedirectTo: safeAddress(redirect), Networks: networks}
				if strings.Contains(target, "$") {
					r.Note = "Dynamic upstream variable requires request-time resolution"
				}
				key, _ := json.Marshal(r)
				r.ID = StableID("route", string(key))
				routes = append(routes, r)
				for _, d := range items {
					if d.name == "location" {
						p := d.args
						if len(p) > 1 && (p[0] == "=" || p[0] == "^~" || p[0] == "~" || p[0] == "~*") {
							p = p[1:]
						}
						emit(d.children, ownRoot, p)
					}
				}
			}
			emit(server.children, rootPath, nil)
		}
	}
	return routes, nil
}
