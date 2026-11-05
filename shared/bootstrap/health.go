package bootstrap

import(
 "net/http"
 "sync/atomic"
)

// ReadinessMiddleware 不影响存活探针和业务路由。
func ReadinessMiddleware(next http.Handler,ready *atomic.Bool)http.Handler{
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/health/ready"&&!ready.Load(){
   w.Header().Set("Content-Type","application/json; charset=utf-8")
   w.Header().Set("Cache-Control","no-store")
   w.WriteHeader(http.StatusServiceUnavailable)
   _,_=w.Write([]byte("{\"status\":\"DOWN\"}\n"))
   return
  }
  next.ServeHTTP(w,r)
 })
}
