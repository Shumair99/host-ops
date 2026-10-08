package discovery

import (
	"strings"
	"testing"
)

func TestNginxExpandsDumpedIncludesAndKeepsPathBackends(t *testing.T) {
	data := []byte("# configuration file /etc/nginx/nginx.conf:\nhttp { upstream api { server 127.0.0.1:3000; } include /odd/vhosts/*.conf; }\n# configuration file /odd/vhosts/site.conf:\nserver { listen 443 ssl; server_name example.com www.example.com; root /unusual/static; location /api/ { proxy_pass http://api; } location ~ \\.php$ { fastcgi_pass unix:/run/php/fpm.sock; } }\n")
	routes, err := ParseNginx(data, "nginx", "proxy", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 || routes[0].Root != "/unusual/static" || routes[1].Upstreams[0] != "127.0.0.1:3000" || routes[2].Upstreams[0] != "unix:/run/php/fpm.sock" {
		t.Fatalf("routing was flattened incorrectly: %+v", routes)
	}
}

func TestTraefikPatternDoesNotPersistUnrelatedHeaderSecrets(t *testing.T) {
	hosts, _, _ := TraefikRule("Header(`Authorization`, `Bearer secret`) && PathPrefix(`/api`)")
	if len(hosts) != 1 || hosts[0] != "*" || strings.Contains(strings.Join(hosts, ""), "secret") {
		t.Fatal("a raw rule was persisted as a domain pattern")
	}
}

func TestTraefikExplicitHostsAndSecretsProjection(t *testing.T) {
	routes, err := ParseTraefik([]byte(`{"routers":{"web@docker":{"rule":"Host(`+"`"+`example.com`+"`"+`) && PathPrefix(`+"`"+`/api`+"`"+`)","service":"web@docker","status":"enabled","tls":{}}},"services":{"web@docker":{"loadBalancer":{"servers":[{"url":"http://user:secret@web:80/?token=secret"}]}}}}`), "t", "proxy", []string{"docker:web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Hosts[0] != "example.com" || routes[0].Scheme != "https" || strings.Contains(routes[0].Upstreams[0], "secret") {
		t.Fatalf("bad route or secret leaked: %+v", routes)
	}
}

func TestApacheVirtualHostOutsideConventionalDirectories(t *testing.T) {
	routes := ParseApache([]byte("<VirtualHost *:443>\nServerName example.com\nServerAlias www.example.com\nDocumentRoot /odd/content\nProxyPass /api http://127.0.0.1:9000/\n</VirtualHost>"), "apache", "p", nil)
	if len(routes) != 1 || len(routes[0].Hosts) != 2 || routes[0].Upstreams[0] != "http://127.0.0.1:9000/" {
		t.Fatal(routes)
	}
}
