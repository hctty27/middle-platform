package bootstrap
import("net/http";"net/http/httptest";"sync/atomic";"testing")

func TestReadiness(t *testing.T){
 var ready atomic.Bool
 h:=ReadinessMiddleware(http.HandlerFunc(func(w http.ResponseWriter,_ *http.Request){w.WriteHeader(200)}),&ready)
 r:=httptest.NewRecorder()
 h.ServeHTTP(r,httptest.NewRequest("GET","/health/ready",nil))
 if r.Code!=503{t.Fatal(r.Code)}
 ready.Store(true)
 r=httptest.NewRecorder()
 h.ServeHTTP(r,httptest.NewRequest("GET","/health/ready",nil))
 if r.Code!=200{t.Fatal(r.Code)}
}
