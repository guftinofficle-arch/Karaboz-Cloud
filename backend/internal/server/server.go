package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"database/sql"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/argon2"
)

type Server struct {
	db *sql.DB
	store *s3.Client
	jwtSecret []byte
	maxUpload int64
	quotaBytes int64
	bucketLimit int
	origin string
}

type userClaims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

type apiCredential struct {
	ProjectID string
	KeyID string
	Permission string
}

var bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

func New(db *sql.DB, store *s3.Client, jwtSecret string, maxUpload int64, origin string) *Server {
	quota := int64(1 << 30)
	if n, err := strconv.ParseInt(os.Getenv("DEFAULT_PROJECT_QUOTA_BYTES"), 10, 64); err == nil && n > 0 {
		quota = n
	}
	buckets := 10
	if n, err := strconv.Atoi(os.Getenv("DEFAULT_PROJECT_BUCKET_LIMIT")); err == nil && n > 0 && n <= 1000 {
		buckets = n
	}
	return &Server{db: db, store: store, jwtSecret: []byte(jwtSecret), maxUpload: maxUpload, quotaBytes: quota, bucketLimit: buckets, origin: origin}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /v1/auth/register", s.register)
	mux.HandleFunc("POST /v1/auth/login", s.login)
	mux.HandleFunc("GET /v1/projects", s.withUser(s.listProjects))
	mux.HandleFunc("POST /v1/projects", s.withUser(s.createProject))
	mux.HandleFunc("POST /v1/projects/{projectID}/keys", s.withUser(s.createKey))
	mux.HandleFunc("GET /v1/projects/{projectID}/keys", s.withUser(s.listKeys))
	mux.HandleFunc("DELETE /v1/projects/{projectID}/keys/{keyID}", s.withUser(s.revokeKey))
	mux.HandleFunc("POST /v1/buckets", s.withAPIKey(s.createBucket))
	mux.HandleFunc("GET /v1/buckets", s.withAPIKey(s.listBuckets))
	mux.HandleFunc("PUT /v1/objects", s.withAPIKey(s.uploadObject))
	mux.HandleFunc("GET /v1/objects", s.withAPIKey(s.listObjects))
	mux.HandleFunc("GET /v1/objects/download", s.withAPIKey(s.downloadObject))
	mux.HandleFunc("POST /v1/objects/presign", s.withAPIKey(s.presignObject))
	mux.HandleFunc("DELETE /v1/objects", s.withAPIKey(s.deleteObject))
	mux.HandleFunc("GET /v1/usage", s.withAPIKey(s.usage))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" && s.origin != "" && origin == s.origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Karaboz-Key")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "karaboz-cloud-api"})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
		Email string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) { return }
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if len(req.DisplayName) < 2 || len(req.DisplayName) > 80 || !strings.Contains(req.Email, "@") || len(req.Email) > 254 || len(req.Password) < 10 || len(req.Password) > 128 {
		writeError(w, http.StatusBadRequest, "enter a valid name, email and password of 10-128 characters")
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not secure password")
		return
	}
	var userID string
	err = s.db.QueryRowContext(r.Context(), "INSERT INTO developer_users(email,display_name,password_hash) VALUES($1,$2,$3) RETURNING id::text", req.Email, req.DisplayName, hash).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusConflict, "an account with this email may already exist")
		return
	}
	token, err := s.issueJWT(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user": map[string]string{"id": userID, "email": req.Email, "display_name": req.DisplayName}})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct { Email string `json:"email"`; Password string `json:"password"` }
	if !decodeJSON(w, r, &req) { return }
	var userID, savedHash string
	err := s.db.QueryRowContext(r.Context(), "SELECT id::text,password_hash FROM developer_users WHERE email=$1", strings.ToLower(strings.TrimSpace(req.Email))).Scan(&userID, &savedHash)
	if err != nil || !verifyPassword(req.Password, savedHash) {
		writeError(w, http.StatusUnauthorized, "email or password is incorrect")
		return
	}
	token, err := s.issueJWT(userID)
	if err != nil { writeError(w, http.StatusInternalServerError, "could not create session"); return }
	writeJSON(w, http.StatusOK, map[string]any{"token": token})
}

func (s *Server) withUser(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if raw == "" { writeError(w, http.StatusUnauthorized, "sign in is required"); return }
		claims := &userClaims{}
		token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 { return nil, errors.New("unexpected signing method") }
			return s.jwtSecret, nil
		}, jwt.WithExpirationRequired())
		if err != nil || !token.Valid || claims.UserID == "" {
			writeError(w, http.StatusUnauthorized, "session is invalid or expired")
			return
		}
		next(w, r, claims.UserID)
	}
}

func (s *Server) issueJWT(userID string) (string, error) {
	now := time.Now()
	claims := userClaims{UserID: userID, RegisteredClaims: jwt.RegisteredClaims{Subject: userID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(24*time.Hour)), Issuer: "karaboz-cloud"}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request, userID string) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT p.id::text,p.name,p.storage_limit_bytes,p.bucket_limit,p.created_at,
		COALESCE((SELECT SUM(o.size_bytes) FROM buckets b JOIN objects o ON o.bucket_id=b.id WHERE b.project_id=p.id),0),
		(SELECT COUNT(*) FROM buckets b WHERE b.project_id=p.id)
		FROM projects p WHERE p.user_id=$1 ORDER BY p.created_at DESC`, userID)
	if err != nil { writeError(w, 500, "could not list projects"); return }
	defer rows.Close()
	projects := make([]map[string]any, 0)
	for rows.Next() {
		var id, name string
		var quota, used int64
		var bucketLimit, bucketCount int
		var created time.Time
		if err := rows.Scan(&id,&name,&quota,&bucketLimit,&created,&used,&bucketCount); err != nil { writeError(w,500,"could not read projects"); return }
		projects = append(projects,map[string]any{"id":id,"name":name,"storage_limit_bytes":quota,"bucket_limit":bucketLimit,"storage_used_bytes":used,"bucket_count":bucketCount,"created_at":created})
	}
	if rows.Err()!=nil { writeError(w,500,"could not read projects"); return }
	writeJSON(w,200,map[string]any{"projects":projects})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct { Name string `json:"name"` }
	if !decodeJSON(w,r,&req) { return }
	req.Name=strings.TrimSpace(req.Name)
	if len(req.Name)<2 || len(req.Name)>80 { writeError(w,400,"project name must be 2-80 characters"); return }
	var id string
	err:=s.db.QueryRowContext(r.Context(),"INSERT INTO projects(user_id,name,storage_limit_bytes,bucket_limit) VALUES($1,$2,$3,$4) RETURNING id::text",userID,req.Name,s.quotaBytes,s.bucketLimit).Scan(&id)
	if err!=nil { writeError(w,500,"could not create project"); return }
	writeJSON(w,201,map[string]any{"project":map[string]any{"id":id,"name":req.Name,"storage_limit_bytes":s.quotaBytes,"bucket_limit":s.bucketLimit,"storage_used_bytes":0,"bucket_count":0}})
}

func (s *Server) checkProjectOwner(ctx context.Context, userID, projectID string) bool {
	var exists bool
	err:=s.db.QueryRowContext(ctx,"SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND user_id=$2)",projectID,userID).Scan(&exists)
	return err==nil && exists
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request, userID string) {
	projectID:=r.PathValue("projectID")
	if !s.checkProjectOwner(r.Context(),userID,projectID) { writeError(w,404,"project not found"); return }
	var req struct { Name string `json:"name"`; Permission string `json:"permission"` }
	if !decodeJSON(w,r,&req) { return }
	req.Name=strings.TrimSpace(req.Name); if req.Name=="" { req.Name="Application key" }; if len(req.Name)>60 { writeError(w,400,"key name is too long"); return }
	if req.Permission=="" { req.Permission="read_write" }
	if req.Permission!="read_write" && req.Permission!="read_only" { writeError(w,400,"permission must be read_write or read_only"); return }
	raw,err:=randomKey(); if err!=nil { writeError(w,500,"could not generate API key"); return }
	sum:=sha256.Sum256([]byte(raw)); hash:=hex.EncodeToString(sum[:]); prefix:=raw[:18]
	var id string
	err=s.db.QueryRowContext(r.Context(),"INSERT INTO api_keys(project_id,name,key_prefix,key_hash,permission) VALUES($1,$2,$3,$4,$5) RETURNING id::text",projectID,req.Name,prefix,hash,req.Permission).Scan(&id)
	if err!=nil { writeError(w,500,"could not create API key"); return }
	writeJSON(w,201,map[string]any{"id":id,"name":req.Name,"key_prefix":prefix,"permission":req.Permission,"api_key":raw,"warning":"Copy this secret now. It cannot be retrieved again."})
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request, userID string) {
	projectID:=r.PathValue("projectID")
	if !s.checkProjectOwner(r.Context(),userID,projectID) { writeError(w,404,"project not found"); return }
	rows,err:=s.db.QueryContext(r.Context(),"SELECT id::text,name,key_prefix,permission,created_at,last_used_at,revoked_at FROM api_keys WHERE project_id=$1 ORDER BY created_at DESC",projectID)
	if err!=nil { writeError(w,500,"could not list API keys"); return }; defer rows.Close()
	keys:=make([]map[string]any,0)
	for rows.Next() { var id,name,prefix,permission string; var created time.Time; var last,revoked sql.NullTime
		if err:=rows.Scan(&id,&name,&prefix,&permission,&created,&last,&revoked);err!=nil {writeError(w,500,"could not read API keys");return}
		keys=append(keys,map[string]any{"id":id,"name":name,"key_prefix":prefix,"permission":permission,"created_at":created,"last_used_at":nullableTime(last),"revoked_at":nullableTime(revoked)})
	}
	writeJSON(w,200,map[string]any{"keys":keys})
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request, userID string) {
	projectID,keyID:=r.PathValue("projectID"),r.PathValue("keyID")
	result,err:=s.db.ExecContext(r.Context(),"UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND project_id=$2 AND project_id IN (SELECT id FROM projects WHERE user_id=$3) AND revoked_at IS NULL",keyID,projectID,userID)
	if err!=nil {writeError(w,500,"could not revoke API key");return}
	n,_:=result.RowsAffected(); if n==0 {writeError(w,404,"active API key not found");return}
	writeJSON(w,200,map[string]any{"revoked":true})
}

func (s *Server) withAPIKey(next func(http.ResponseWriter,*http.Request,apiCredential)) http.HandlerFunc {
	return func(w http.ResponseWriter,r *http.Request) {
		raw:=strings.TrimSpace(r.Header.Get("X-Karaboz-Key"))
		if raw=="" { raw=strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer ")) }
		if raw=="" {writeError(w,401,"project API key is required");return}
		sum:=sha256.Sum256([]byte(raw)); hash:=hex.EncodeToString(sum[:])
		var cred apiCredential
		err:=s.db.QueryRowContext(r.Context(),"SELECT id::text,project_id::text,permission FROM api_keys WHERE key_hash=$1 AND revoked_at IS NULL",hash).Scan(&cred.KeyID,&cred.ProjectID,&cred.Permission)
		if err!=nil {writeError(w,401,"API key is invalid or revoked");return}
		_,_=s.db.ExecContext(r.Context(),"UPDATE api_keys SET last_used_at=now() WHERE id=$1",cred.KeyID)
		next(w,r,cred)
	}
}

func (s *Server) createBucket(w http.ResponseWriter,r *http.Request,cred apiCredential) {
	if cred.Permission!="read_write" {writeError(w,403,"this API key is read-only");return}
	var req struct{Name string `json:"name"`}
	if !decodeJSON(w,r,&req){return}; req.Name=strings.ToLower(strings.TrimSpace(req.Name))
	if !bucketNamePattern.MatchString(req.Name) {writeError(w,400,"bucket name must be 3-63 lowercase letters, numbers or hyphens");return}
	var count,limit int
	if err:=s.db.QueryRowContext(r.Context(),"SELECT COUNT(b.id),p.bucket_limit FROM projects p LEFT JOIN buckets b ON b.project_id=p.id WHERE p.id=$1 GROUP BY p.bucket_limit",cred.ProjectID).Scan(&count,&limit);err!=nil && !errors.Is(err,sql.ErrNoRows){writeError(w,500,"could not check bucket limit");return}
	if count>=limit && limit>0 {writeError(w,409,"project bucket limit reached");return}
	storageName,err:=physicalBucketName();if err!=nil{writeError(w,500,"could not allocate storage bucket");return}
	_,err=s.store.CreateBucket(r.Context(),&s3.CreateBucketInput{Bucket:aws.String(storageName)})
	if err!=nil {log.Printf("S3 create bucket: %v",err);writeError(w,502,"storage service could not create the bucket");return}
	var id string
	err=s.db.QueryRowContext(r.Context(),"INSERT INTO buckets(project_id,name,storage_name) VALUES($1,$2,$3) RETURNING id::text",cred.ProjectID,req.Name,storageName).Scan(&id)
	if err!=nil {_,_=s.store.DeleteBucket(r.Context(),&s3.DeleteBucketInput{Bucket:aws.String(storageName)});writeError(w,409,"bucket already exists or could not be recorded");return}
	writeJSON(w,201,map[string]any{"id":id,"name":req.Name})
}

func (s *Server) listBuckets(w http.ResponseWriter,r *http.Request,cred apiCredential) {
	rows,err:=s.db.QueryContext(r.Context(),"SELECT id::text,name,created_at FROM buckets WHERE project_id=$1 ORDER BY created_at DESC",cred.ProjectID)
	if err!=nil{writeError(w,500,"could not list buckets");return};defer rows.Close()
	items:=make([]map[string]any,0)
	for rows.Next(){var id,name string;var created time.Time;if err:=rows.Scan(&id,&name,&created);err!=nil{writeError(w,500,"could not read buckets");return};items=append(items,map[string]any{"id":id,"name":name,"created_at":created})}
	writeJSON(w,200,map[string]any{"buckets":items})
}

func (s *Server) lookupBucket(ctx context.Context,projectID,name string)(string,string,error){
	var id,physical string
	err:=s.db.QueryRowContext(ctx,"SELECT id::text,storage_name FROM buckets WHERE project_id=$1 AND name=$2",projectID,name).Scan(&id,&physical)
	return id,physical,err
}

func validObjectKey(key string) bool {
	if key=="" || len(key)>1024 || strings.HasPrefix(key,"/") || strings.ContainsRune(key,'\x00') {return false}
	for _,part:=range strings.Split(key,"/"){if part=="."||part==".."{return false}}
	return true
}

func (s *Server) uploadObject(w http.ResponseWriter,r *http.Request,cred apiCredential) {
	if cred.Permission!="read_write"{writeError(w,403,"this API key is read-only");return}
	bucketName,key:=r.URL.Query().Get("bucket"),r.URL.Query().Get("key")
	if !validObjectKey(key){writeError(w,400,"a valid object key is required");return}
	bucketID,physical,err:=s.lookupBucket(r.Context(),cred.ProjectID,bucketName)
	if err!=nil{writeError(w,404,"bucket not found");return}
	r.Body=http.MaxBytesReader(w,r.Body,s.maxUpload)
	body,err:=io.ReadAll(r.Body)
	if err!=nil{var maxErr *http.MaxBytesError;if errors.As(err,&maxErr){writeError(w,413,"file exceeds the upload size limit");return};writeError(w,400,"could not read upload");return}
	var oldSize int64
	_ = s.db.QueryRowContext(r.Context(),"SELECT size_bytes FROM objects WHERE bucket_id=$1 AND object_key=$2",bucketID,key).Scan(&oldSize)
	var used,quota int64
	if err:=s.db.QueryRowContext(r.Context(),"SELECT COALESCE(SUM(o.size_bytes),0),p.storage_limit_bytes FROM projects p LEFT JOIN buckets b ON b.project_id=p.id LEFT JOIN objects o ON o.bucket_id=b.id WHERE p.id=$1 GROUP BY p.storage_limit_bytes",cred.ProjectID).Scan(&used,&quota);err!=nil{writeError(w,500,"could not check storage quota");return}
	if used-oldSize+int64(len(body))>quota{writeError(w,413,"project storage quota exceeded");return}
	contentType:=r.Header.Get("Content-Type");if contentType==""{contentType="application/octet-stream"}
	put,err:=s.store.PutObject(r.Context(),&s3.PutObjectInput{Bucket:aws.String(physical),Key:aws.String(key),Body:bytes.NewReader(body),ContentLength:aws.Int64(int64(len(body))),ContentType:aws.String(contentType)})
	if err!=nil{log.Printf("S3 put object: %v",err);writeError(w,502,"storage service could not save the file");return}
	etag:="";if put.ETag!=nil{etag=strings.Trim(*put.ETag,"\"")}
	_,err=s.db.ExecContext(r.Context(),`INSERT INTO objects(bucket_id,object_key,size_bytes,content_type,etag) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(bucket_id,object_key) DO UPDATE SET size_bytes=EXCLUDED.size_bytes,content_type=EXCLUDED.content_type,etag=EXCLUDED.etag,updated_at=now()`,bucketID,key,len(body),contentType,etag)
	if err!=nil{writeError(w,500,"file saved but metadata update failed; contact the service operator");return}
	writeJSON(w,201,map[string]any{"bucket":bucketName,"key":key,"size_bytes":len(body),"content_type":contentType,"etag":etag})
}

func (s *Server) listObjects(w http.ResponseWriter,r *http.Request,cred apiCredential) {
	bucketID,_,err:=s.lookupBucket(r.Context(),cred.ProjectID,r.URL.Query().Get("bucket"));if err!=nil{writeError(w,404,"bucket not found");return}
	rows,err:=s.db.QueryContext(r.Context(),"SELECT object_key,size_bytes,content_type,etag,updated_at FROM objects WHERE bucket_id=$1 ORDER BY updated_at DESC LIMIT 500",bucketID)
	if err!=nil{writeError(w,500,"could not list files");return};defer rows.Close()
	items:=make([]map[string]any,0)
	for rows.Next(){var key,contentType,etag string;var size int64;var updated time.Time;if err:=rows.Scan(&key,&size,&contentType,&etag,&updated);err!=nil{writeError(w,500,"could not read files");return};items=append(items,map[string]any{"key":key,"size_bytes":size,"content_type":contentType,"etag":etag,"updated_at":updated})}
	writeJSON(w,200,map[string]any{"objects":items,"limit":500})
}

func (s *Server) downloadObject(w http.ResponseWriter,r *http.Request,cred apiCredential) {
	bucketID,physical,err:=s.lookupBucket(r.Context(),cred.ProjectID,r.URL.Query().Get("bucket"));if err!=nil{writeError(w,404,"bucket not found");return}
	key:=r.URL.Query().Get("key");if !validObjectKey(key){writeError(w,400,"a valid object key is required");return}
	var exists bool;_ = s.db.QueryRowContext(r.Context(),"SELECT EXISTS(SELECT 1 FROM objects WHERE bucket_id=$1 AND object_key=$2)",bucketID,key).Scan(&exists);if !exists{writeError(w,404,"file not found");return}
	obj,err:=s.store.GetObject(r.Context(),&s3.GetObjectInput{Bucket:aws.String(physical),Key:aws.String(key)})
	if err!=nil{writeError(w,404,"file not found");return};defer obj.Body.Close()
	if obj.ContentType!=nil{w.Header().Set("Content-Type",*obj.ContentType)}else{w.Header().Set("Content-Type","application/octet-stream")}
	if obj.ContentLength!=nil{w.Header().Set("Content-Length",strconv.FormatInt(*obj.ContentLength,10))}
	w.Header().Set("Content-Disposition","attachment")
	w.WriteHeader(http.StatusOK);if _,err:=io.Copy(w,obj.Body);err!=nil{log.Printf("download stream: %v",err)}
}

func (s *Server) presignObject(w http.ResponseWriter,r *http.Request,cred apiCredential) {
	bucketID,physical,err:=s.lookupBucket(r.Context(),cred.ProjectID,r.URL.Query().Get("bucket"));if err!=nil{writeError(w,404,"bucket not found");return}
	key:=r.URL.Query().Get("key");if !validObjectKey(key){writeError(w,400,"a valid object key is required");return}
	var exists bool;_ = s.db.QueryRowContext(r.Context(),"SELECT EXISTS(SELECT 1 FROM objects WHERE bucket_id=$1 AND object_key=$2)",bucketID,key).Scan(&exists);if !exists{writeError(w,404,"file not found");return}
	presigner:=s3.NewPresignClient(s.store)
	signed,err:=presigner.PresignGetObject(r.Context(),&s3.GetObjectInput{Bucket:aws.String(physical),Key:aws.String(key)},s3.WithPresignExpires(15*time.Minute))
	if err!=nil{writeError(w,502,"could not create a download link");return}
	writeJSON(w,200,map[string]any{"url":signed.URL,"expires_in_seconds":900})
}

func (s *Server) deleteObject(w http.ResponseWriter,r *http.Request,cred apiCredential) {
	if cred.Permission!="read_write"{writeError(w,403,"this API key is read-only");return}
	bucketID,physical,err:=s.lookupBucket(r.Context(),cred.ProjectID,r.URL.Query().Get("bucket"));if err!=nil{writeError(w,404,"bucket not found");return}
	key:=r.URL.Query().Get("key");if !validObjectKey(key){writeError(w,400,"a valid object key is required");return}
	_,err=s.store.DeleteObject(r.Context(),&s3.DeleteObjectInput{Bucket:aws.String(physical),Key:aws.String(key)})
	if err!=nil{writeError(w,502,"storage service could not delete the file");return}
	_,_=s.db.ExecContext(r.Context(),"DELETE FROM objects WHERE bucket_id=$1 AND object_key=$2",bucketID,key)
	writeJSON(w,200,map[string]any{"deleted":true})
}

func (s *Server) usage(w http.ResponseWriter,r *http.Request,cred apiCredential) {
	var used,quota int64;var objects,buckets int
	err:=s.db.QueryRowContext(r.Context(),`SELECT p.storage_limit_bytes,
		COALESCE((SELECT SUM(o.size_bytes) FROM buckets b JOIN objects o ON o.bucket_id=b.id WHERE b.project_id=p.id),0),
		(SELECT COUNT(*) FROM objects o JOIN buckets b ON b.id=o.bucket_id WHERE b.project_id=p.id),
		(SELECT COUNT(*) FROM buckets b WHERE b.project_id=p.id)
		FROM projects p WHERE p.id=$1`,cred.ProjectID).Scan(&quota,&used,&objects,&buckets)
	if err!=nil{writeError(w,500,"could not calculate usage");return}
	writeJSON(w,200,map[string]any{"storage_used_bytes":used,"storage_limit_bytes":quota,"object_count":objects,"bucket_count":buckets})
}

func decodeJSON(w http.ResponseWriter,r *http.Request,dst any) bool {
	r.Body=http.MaxBytesReader(w,r.Body,1<<20)
	dec:=json.NewDecoder(r.Body);dec.DisallowUnknownFields()
	if err:=dec.Decode(dst);err!=nil{writeError(w,400,"invalid JSON request");return false}
	return true
}
func writeJSON(w http.ResponseWriter,status int,value any){w.Header().Set("Content-Type","application/json");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(value)}
func writeError(w http.ResponseWriter,status int,message string){writeJSON(w,status,map[string]string{"error":message})}
func hashPassword(password string)(string,error){salt:=make([]byte,16);if _,err:=rand.Read(salt);err!=nil{return "",err};hash:=argon2.IDKey([]byte(password),salt,3,64*1024,1,32);return base64.RawStdEncoding.EncodeToString(salt)+":"+base64.RawStdEncoding.EncodeToString(hash),nil}
func verifyPassword(password,saved string)bool{parts:=strings.Split(saved,":");if len(parts)!=2{return false};salt,err:=base64.RawStdEncoding.DecodeString(parts[0]);if err!=nil{return false};expected,err:=base64.RawStdEncoding.DecodeString(parts[1]);if err!=nil{return false};actual:=argon2.IDKey([]byte(password),salt,3,64*1024,1,32);return subtle.ConstantTimeCompare(actual,expected)==1}
func randomKey()(string,error){raw:=make([]byte,32);if _,err:=rand.Read(raw);err!=nil{return "",err};return "kbc_live_"+base64.RawURLEncoding.EncodeToString(raw),nil}
func physicalBucketName()(string,error){raw:=make([]byte,16);if _,err:=rand.Read(raw);err!=nil{return "",err};return "kbc-"+hex.EncodeToString(raw),nil}
func nullableTime(v sql.NullTime)any{if !v.Valid{return nil};return v.Time}
func ensureContext(ctx context.Context)error{select{case<-ctx.Done():return ctx.Err();default:return nil}}
var _ = fmt.Sprintf
