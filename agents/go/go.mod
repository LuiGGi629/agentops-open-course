module github.com/MLOps-Courses/agentops-open-course-go/agents/go

go 1.26.5

// --8<-- [start:runtime-dependencies]
require (
	github.com/caarlos0/env/v11 v11.4.1
	github.com/glebarez/go-sqlite v1.23.0
	github.com/glebarez/sqlite v1.11.0
	github.com/google/jsonschema-go v0.4.3
	github.com/modelcontextprotocol/go-sdk v1.7.0
	// ADK Go v2.1.0 owns this generated-client pair. openai-go v3.50.0 changes
	// function-call arguments to a union type and makes ADK's adapter fail to compile.
	github.com/openai/openai-go/v3 v3.8.1 // compatibility hold: owner=google.golang.org/adk/v2@v2.1.0 constraint=v3.8.1 validator=agents/go mise run check and test
	// ADK Go v2.1.0 uses the OTel log.Value API removed by the 1.45/0.21
	// release set, so 1.44 with log 0.19 is the highest compiling family.
	go.opentelemetry.io/otel v1.44.0 // compatibility hold: owner=google.golang.org/adk/v2@v2.1.0 constraint=v1.44.0 validator=agents/go mise run check and test
	go.opentelemetry.io/otel/metric v1.44.0
	go.opentelemetry.io/otel/sdk/metric v1.44.0
	golang.org/x/text v0.40.0
	google.golang.org/adk/v2 v2.1.0
	google.golang.org/genai v1.63.0 // compatibility hold: owner=google.golang.org/adk/v2@v2.1.0 constraint=v1.63.0 validator=agents/go mise run check and test
)

// --8<-- [end:runtime-dependencies]

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.35.0 // indirect
	github.com/a2aproject/a2a-go v0.3.15 // indirect
	github.com/a2aproject/a2a-go/v2 v2.4.0
	github.com/awalterschulze/gographviz v2.0.3+incompatible // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/safehtml v0.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.15 // indirect
	github.com/googleapis/gax-go/v2 v2.22.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/detectors/gcp v1.44.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.19.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.44.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.44.0 // indirect
	go.opentelemetry.io/otel/log v0.19.0 // compatibility hold: owner=google.golang.org/adk/v2@v2.1.0 constraint=v0.19.0 validator=agents/go mise run check and test
	go.opentelemetry.io/otel/sdk v1.44.0
	go.opentelemetry.io/otel/sdk/log v0.19.0
	go.opentelemetry.io/otel/trace v1.44.0
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/telemetry v0.0.0-20260708182218-49f421fb7959 // indirect
	golang.org/x/tools v0.48.0 // indirect
	golang.org/x/vuln v1.6.0 // indirect
	google.golang.org/api v0.279.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/grpc v1.83.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gorm.io/gorm v1.31.0 // indirect
	modernc.org/libc v1.74.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.55.0 // indirect
	mvdan.cc/gofumpt v0.11.0 // indirect
	rsc.io/omap v1.2.0 // indirect
	rsc.io/ordered v1.1.1 // indirect
)

require golang.org/x/time v0.15.0 // indirect

tool (
	golang.org/x/tools/cmd/goimports
	golang.org/x/vuln/cmd/govulncheck
	mvdan.cc/gofumpt
)
