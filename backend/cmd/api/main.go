package main
import (
 "context"
 "database/sql"
 "log"
 "net/http"
 "os"
 "strconv"
 "strings"
 "time"
 "github.com/aws/aws-sdk-go-v2/aws"
 awsconfig "github.com/aws/aws-sdk-go-v2/config"
 "github.com/aws/aws-sdk-go-v2/credentials"
 "github.com/aws/aws-sdk-go-v2/service/s3"
 _ "github.com/jackc/pgx/v5/stdlib"
 dbmigrations "github.com/guftinofficle-arch/karaboz-cloud/backend/internal/db"
 "github.com/guftinofficle-arch/karaboz-cloud/backend/internal/server"
)
func main() {
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second); defer cancel()
 databaseURL:=requiredEnv("DATABASE_URL"); jwtSecret:=requiredEnv("JWT_SECRET")
 if len(jwtSecret)<32 { log.Fatal("JWT_SECRET must contain at least 32 characters") }
 conn,err:=sql.Open("pgx",databaseURL); if err!=nil {log.Fatalf("open database: %v",err)}; defer conn.Close()
 conn.SetMaxOpenConns(20); conn.SetMaxIdleConns(5); conn.SetConnMaxLifetime(30*time.Minute)
 if err:=conn.PingContext(ctx);err!=nil {log.Fatalf("connect database: %v",err)}
 if err:=dbmigrations.Apply(ctx,conn);err!=nil {log.Fatalf("database migration: %v",err)}
 awsCfg,err:=awsconfig.LoadDefaultConfig(ctx,awsconfig.WithRegion(envOr("S3_REGION","us-east-1")),awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(requiredEnv("S3_ACCESS_KEY"),requiredEnv("S3_SECRET_KEY"),"")))
 if err!=nil {log.Fatalf("load S3 config: %v",err)}
 s3Client:=s3.NewFromConfig(awsCfg,func(o *s3.Options){o.BaseEndpoint=aws.String(requiredEnv("S3_ENDPOINT"));o.UsePathStyle=true})
 maxUpload:=int64(25<<20); if raw:=os.Getenv("MAX_UPLOAD_BYTES");raw!="" {if n,e:=strconv.ParseInt(raw,10,64);e==nil&&n>0&&n<=250<<20 {maxUpload=n}}
 api:=server.New(conn,s3Client,jwtSecret,maxUpload,envOr("CORS_ALLOWED_ORIGIN",""))
 addr:=envOr("API_ADDR",":8080")
 httpServer:=&http.Server{Addr:addr,Handler:api.Handler(),ReadHeaderTimeout:10*time.Second,ReadTimeout:30*time.Second,WriteTimeout:2*time.Minute,IdleTimeout:60*time.Second}
 log.Printf("Karaboz Cloud API listening on %s",addr)
 if err:=httpServer.ListenAndServe();err!=nil&&err!=http.ErrServerClosed {log.Fatalf("HTTP server: %v",err)}
}
func requiredEnv(key string)string{value:=strings.TrimSpace(os.Getenv(key));if value==""{log.Fatalf("%s is required",key)};return value}
func envOr(key,fallback string)string{if value:=strings.TrimSpace(os.Getenv(key));value!=""{return value};return fallback}
