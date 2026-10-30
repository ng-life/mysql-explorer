package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ng-life/mysql-explorer/internal/config"
	"github.com/ng-life/mysql-explorer/internal/mysql"
	"github.com/ng-life/mysql-explorer/internal/web"
)

type provider interface {
	Get(context.Context, int) (mysql.Reader, error)
}

// Server serves the browser with connection-scoped SQL permissions.
type Server struct {
	config config.Config
	pool   provider
}

// New creates HTTP handlers with a lazily connected provider.
func New(c config.Config, pool provider) *Server { return &Server{c, pool} }
func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

// Handler returns the browser and API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", s.mcpHandler())
	mux.Handle("GET /assets/", http.FileServerFS(web.Assets))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(web.IndexHTML)
	})
	mux.HandleFunc("GET /api/clusters", func(w http.ResponseWriter, r *http.Request) {
		type publicCluster struct {
			Name     string `json:"name"`
			Address  string `json:"address"`
			Type     string `json:"type"`
			AllowDML bool   `json:"allow_dml"`
			AllowDDL bool   `json:"allow_ddl"`
		}
		out := []publicCluster{}
		for _, cl := range s.config.Clusters {
			out = append(out, publicCluster{cl.Name, cl.Address, cl.Type, cl.AllowDML, cl.AllowDDL})
		}
		respond(w, out)
	})
	mux.HandleFunc("GET /api/clusters/{id}/databases", s.databases)
	mux.HandleFunc("GET /api/clusters/{id}/metadata", s.metadata)
	mux.HandleFunc("POST /api/clusters/{id}/metadata/refresh", s.refreshMetadata)
	mux.HandleFunc("GET /api/clusters/{id}/tables", s.tables)
	mux.HandleFunc("GET /api/clusters/{id}/columns", s.columns)
	mux.HandleFunc("POST /api/clusters/{id}/rows/query", s.rows)
	return mux
}
func (s *Server) reader(w http.ResponseWriter, r *http.Request) (mysql.Reader, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 0 || id >= len(s.config.Clusters) {
		http.Error(w, "未知的 MySQL 服务", 404)
		return nil, false
	}
	client, err := s.pool.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "无法连接 MySQL，请检查地址、认证和网络后重试", 502)
		return nil, false
	}
	return client, true
}
func bounded(r *http.Request) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	return r.WithContext(ctx), cancel
}
func (s *Server) databases(w http.ResponseWriter, r *http.Request) {
	r, cancel := bounded(r)
	defer cancel()
	client, ok := s.reader(w, r)
	if !ok {
		return
	}
	names, err := client.Databases(r.Context())
	if err != nil {
		http.Error(w, "无法读取数据库列表，请检查访问权限", 502)
		return
	}
	respond(w, map[string]any{"databases": names})
}
func (s *Server) tables(w http.ResponseWriter, r *http.Request) {
	db := r.URL.Query().Get("db")
	if !validName(db) {
		http.Error(w, "请选择有效的数据库", 400)
		return
	}
	r, cancel := bounded(r)
	defer cancel()
	client, ok := s.reader(w, r)
	if !ok {
		return
	}
	names, err := client.RefreshTables(r.Context(), db)
	if err != nil {
		http.Error(w, "无法读取 表列表，请检查访问权限", 502)
		return
	}
	respond(w, map[string]any{"tables": names})
}
func validName(name string) bool {
	return name != "" && !strings.ContainsRune(name, 0) && len(name) <= 255
}

type queryRequest struct {
	Mode     string `json:"mode"`
	Database string `json:"database"`
	SQL      string `json:"sql"`
	Limit    int    `json:"limit"`
	Skip     int    `json:"skip"`
}

func (s *Server) rows(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	defer r.Body.Close()
	var in queryRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		http.Error(w, "请求必须是有效的 JSON，且不超过 1 MiB", 400)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "请求只能包含一个 JSON 对象", 400)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "未知的 MySQL 服务", 404)
		return
	}
	out, err := s.runQuery(r.Context(), id, in)
	if err != nil {
		e := err.(*queryError)
		http.Error(w, e.message, e.status)
		return
	}

	respond(w, out)
}

func (s *Server) columns(w http.ResponseWriter, r *http.Request) {
	db, table := r.URL.Query().Get("db"), r.URL.Query().Get("table")
	if !validName(db) || !validName(table) {
		http.Error(w, "请选择数据库和表", 400)
		return
	}
	r, cancel := bounded(r)
	defer cancel()
	client, ok := s.reader(w, r)
	if !ok {
		return
	}
	columns, err := client.RefreshColumns(r.Context(), db, table)
	if err != nil {
		http.Error(w, "无法读取字段信息", 502)
		return
	}
	respond(w, map[string]any{"columns": columns})
}

func (s *Server) metadata(w http.ResponseWriter, r *http.Request) {
	client, ok := s.reader(w, r)
	if !ok {
		return
	}
	catalog := client.Metadata()
	if catalog == nil {
		http.Error(w, "服务元数据未加载，请刷新重试", 503)
		return
	}
	respond(w, catalog)
}
func (s *Server) refreshMetadata(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	client, ok := s.reader(w, r)
	if !ok {
		return
	}
	catalog, err := client.Refresh(ctx)
	if err != nil {
		http.Error(w, "元数据加载失败，已有缓存保持不变，请检查连接和权限", 502)
		return
	}
	respond(w, catalog)
}
