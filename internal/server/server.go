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

// Server serves the browser and exposes only MySQL read operations.
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

// Handler returns the browser and read-only API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(web.IndexHTML)
	})
	mux.HandleFunc("GET /api/clusters", func(w http.ResponseWriter, r *http.Request) {
		type publicCluster struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		}
		out := []publicCluster{}
		for _, cl := range s.config.Clusters {
			out = append(out, publicCluster{cl.Name, cl.Address})
		}
		respond(w, out)
	})
	mux.HandleFunc("GET /api/clusters/{id}/databases", s.databases)
	mux.HandleFunc("GET /api/clusters/{id}/tables", s.tables)
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
	names, err := client.Tables(r.Context(), db)
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
	if in.Database != "" && !validName(in.Database) {
		http.Error(w, "数据库名称无效", 400)
		return
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit < 1 || in.Limit > 200 || in.Skip < 0 || in.Skip > 1000000 {
		http.Error(w, "每页 1–200 条，偏移量 0–1000000", 400)
		return
	}
	if _, err := mysql.ParseSQL(in.SQL); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	r, cancel := bounded(r)
	defer cancel()
	client, ok := s.reader(w, r)
	if !ok {
		return
	}
	out, err := client.Execute(r.Context(), in.Database, mysql.Query{SQL: in.SQL, Limit: in.Limit, Skip: in.Skip})
	if err != nil {
		http.Error(w, "查询失败，请检查 SQL、数据库和权限；结果过大时减少每页条数", 502)
		return
	}
	respond(w, out)
}
