package collaborforward

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/containous/traefik/v2/pkg/config/dynamic"
	"github.com/containous/traefik/v2/pkg/log"
	"github.com/containous/traefik/v2/pkg/middlewares"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const (
	contentType                    = "Content-Type"
	CoCenterInfoApiPath     string = "/co/center/local"
	NextCoCenterInfoApiPath string = "/co/center/next"
	OptimalNetPathApiPath   string = "/net/path/optimum"
)

var myClient = &http.Client{Timeout: 15 * time.Second}

type collaborForward struct {
	next         http.Handler
	cocoAgentUrl string
	name         string
}

type coCenterInfo struct {
	Code        string
	GatewayIp   string
	GatewayPort string
}

type coCenterInfoMsg struct {
	Status int
	Memo   string
	Result coCenterInfo
}

type optimalPathInfoMsg struct {
	Status int
	Memo   string
	Result string
}

type forwardHandler struct {
	location  *url.URL
	permanent bool
}

type TargetHost struct {
	Host    string
	IsHttps bool
	CAPath  string
}

func New(ctx context.Context, next http.Handler, config dynamic.CollaborForward, name string) (http.Handler, error) {
	log.FromContext(middlewares.GetLoggerCtx(ctx, name, "")).Debug("Creating huawei-login middleware")
	return &collaborForward{
		name:         name,
		cocoAgentUrl: config.CocoAgentUrl,
		next:         next,
	}, nil
}

func (s *collaborForward) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	fmt.Println("collaborForward ServerHttp func() start")
	//define required params
	var gatewayType int //1-source gateway，2-intermediate gateway，3-target gateway
	xRoutePath := req.Header.Get("X-Route-Path")
	nextCoCenterInfo := new(coCenterInfo)
	destCenterCode := req.URL.Query().Get("destCenterCode")
	//destCenterCode := "1510002"
	if destCenterCode == "" {
		exceptionResponse(rw, req)
		return
	}
	//query local collabor center info
	localCoCenterInfo, err := queryCoCenterInfo(s.cocoAgentUrl+CoCenterInfoApiPath, "")
	if err != nil {
		fmt.Println("local collabor center info query failed，please check the reason ...")
		exceptionResponse(rw, req)
		return
	}
	// source gateway should query optimal network path
	if xRoutePath == "" {
		//source gateway - query optimal network path
		optimalPath, err := queryOptimalNetPath(s.cocoAgentUrl+OptimalNetPathApiPath, destCenterCode)
		if err != nil {
			fmt.Println("optimal network path query failed，please check the reason ...")
			exceptionResponse(rw, req)
			return
		}
		req.Header.Set("X-Route-Path", optimalPath)
		xRoutePath = optimalPath
		fmt.Println("optimal network path is :", optimalPath)
	}
	gatewayType, nextCoCenterCode := fetchGatewayType(localCoCenterInfo.Code, xRoutePath)
	if nextCoCenterCode != "" {
		//query next collabor center info
		*nextCoCenterInfo, err = queryCoCenterInfo(s.cocoAgentUrl+NextCoCenterInfoApiPath, nextCoCenterCode)
		if err != nil {
			fmt.Println("next collabor center info query failed，please check the reason ...", err)
			exceptionResponse(rw, req)
			return
		}
	}

	//set XForwardedFor header
	buildXForwardedForHeader(gatewayType, localCoCenterInfo.GatewayIp, req)
	// forward logic
	if gatewayType == 3 {
		//target gateway
		s.next.ServeHTTP(rw, req)
	} else {
		parsedUrl := "http://" + nextCoCenterInfo.GatewayIp + ":" + nextCoCenterInfo.GatewayPort + req.RequestURI
		fmt.Println("RequestURI == ", req.RequestURI)
		fmt.Println("parsedUrl == ", parsedUrl)
		HostReverseProxy(rw, req, parsedUrl)
	}
}

func HostReverseProxy(w http.ResponseWriter, req *http.Request, targetHost string) {
	remote, err := url.Parse(targetHost)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	fmt.Println("parsedUrl == ", remote.String())
	proxy := httputil.NewSingleHostReverseProxy(remote)
	proxy.ServeHTTP(w, req)
}

/*func (f *forwardHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("Location", f.location.String())
	status := http.StatusFound
	if req.Method != http.MethodGet {
		status = http.StatusTemporaryRedirect
	}
	if f.permanent {
		status = http.StatusMovedPermanently
		if req.Method != http.MethodGet {
			status = http.StatusPermanentRedirect
		}
	}
	rw.WriteHeader(status)
	_, err := rw.Write([]byte(http.StatusText(status)))
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
	}
}*/

func queryOptimalNetPath(optimalNetPathUrl string, destCenterCode string) (string, error) {
	optimalPathInfoResultMsg := new(optimalPathInfoMsg)
	//client := &http.Client{}
	req, _ := http.NewRequest("GET", optimalNetPathUrl, nil)
	if destCenterCode != "" {
		q := req.URL.Query()
		q.Add("destCenterCode", destCenterCode)
		req.URL.RawQuery = q.Encode()
	}
	res, _ := myClient.Do(req)
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	json.Unmarshal(body, optimalPathInfoResultMsg)
	return optimalPathInfoResultMsg.Result, nil
}

func queryCoCenterInfo(coCenterUrl string, coCenterCode string) (coCenterInfo, error) {
	coCenterInfoResultMsg := new(coCenterInfoMsg)
	req, _ := http.NewRequest("GET", coCenterUrl, nil)
	if coCenterCode != "" {
		q := req.URL.Query()
		q.Add("coCenterCode", coCenterCode)
		req.URL.RawQuery = q.Encode()
	}
	res, _ := myClient.Do(req)
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return coCenterInfo{}, err
	}
	json.Unmarshal(body, coCenterInfoResultMsg)
	return coCenterInfoResultMsg.Result, nil
}

func fetchGatewayType(coCenterCode string, xRoutePath string) (int, string) {
	var gatewayType int
	var nextCoCenterCode string
	pathArray := strings.Split(xRoutePath, ",")
	index := indexOf(coCenterCode, pathArray)
	if index == 0 {
		nextCoCenterCode = pathArray[index+1]
		gatewayType = 1
	} else if index == len(pathArray)-1 {
		fmt.Println("target gateway has no nextCoCenter ...")
		nextCoCenterCode = ""
		gatewayType = 3
	} else {
		nextCoCenterCode = pathArray[index+1]
		gatewayType = 2
	}
	return gatewayType, nextCoCenterCode
}

func buildXForwardedForHeader(gatewayType int, gatewayIp string, req *http.Request) {
	var xForwardedFor = req.Header.Get("X-Forwarded-For")
	var buffer bytes.Buffer
	if gatewayType == 1 {
		buffer.WriteString(xForwardedFor + gatewayIp)
		req.Header.Set("X-Forwarded-For", buffer.String())
	} else {
		buffer.WriteString(xForwardedFor + "," + gatewayIp)
		req.Header.Set("X-Forwarded-For", buffer.String())
	}
}

func exceptionResponse(w http.ResponseWriter, r *http.Request) {
	var exceptionResponse = fmt.Sprintf("%d %s\n", http.StatusExpectationFailed, http.StatusText(http.StatusExpectationFailed))
	w.Header().Set(contentType, "text/plain")
	w.WriteHeader(http.StatusExpectationFailed)
	w.Write([]byte(exceptionResponse))
}

func indexOf(element string, data []string) int {
	for k, v := range data {
		if element == v {
			return k
		}
	}
	return -1 //not found.
}
