package collaborforward

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/containous/traefik/pkg/log"
	"github.com/dengyun19900722/traefik/pkg/config/dynamic"
	"github.com/dengyun19900722/traefik/pkg/middlewares"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	contentType = "Content-Type"
	CoCenterInfoApiPath string = "/co/center/local"
	NextCoCenterInfoApiPath string = "/co/center/next"
	OptimalNetPathApiPath string = "/net/path/optimum"
)

var myClient = &http.Client{Timeout: 15 * time.Second}

type collaborForward struct {
	next         http.Handler
	cocoAgentUrl string
	name         string
}

type coCenterInfo struct {
	code         string
	gatewayIp    string
	gatewayPort  string
}

type coCenterInfoMsg struct {
	status  int
	memo    string
	result  coCenterInfo
}

type optimalPathInfoMsg struct {
	status  int
	memo    string
	result  string
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
	var gatewayType int //1-source gateway，2-intermediate gateway，3-target gateway
	//query local collabor center info
	localCoCenterInfo, err := queryCoCenterInfo(s.cocoAgentUrl + CoCenterInfoApiPath, "")
	if err != nil {
		fmt.Println("local collabor center info query failed，please check the reason ...")
		exceptionResponse(rw, req)
		return
	}
	gatewayType, nextCoCenterCode := fetchGatewayType(localCoCenterInfo.code, rw.Header().Get("X-Route-Path"))
	//query next collabor center info
	nextCoCenterInfo, err := queryCoCenterInfo(s.cocoAgentUrl + NextCoCenterInfoApiPath, nextCoCenterCode)
	if err != nil {
		fmt.Println("next collabor center info query failed，please check the reason ...", err)
		exceptionResponse(rw, req)
		return
	}
	//queryNextCoCenterInfo(s.cocoAgentUrl + NextCoCenterInfoApiPath)
	var buffer bytes.Buffer
	var xForwardedFor = req.Header.Get("X-Forwarded-For")
	if gatewayType == 1 {
		//source gateway - query optimal network path
		optimalPath, err := queryOptimalNetPath(s.cocoAgentUrl + OptimalNetPathApiPath)
		if err != nil {
			fmt.Println("optimal network path query failed，please check the reason ...")
			exceptionResponse(rw, req)
			return
		}
		buffer.WriteString(xForwardedFor)
		req.Header.Set("X-Route-Path", optimalPath)
		req.Header.Set("X-Forwarded-For", buffer.String())
		http.Redirect(rw, req, nextCoCenterInfo.gatewayIp + nextCoCenterInfo.gatewayPort + req.RequestURI, 308)
	} else if gatewayType == 2 {
		buffer.WriteString(xForwardedFor + "," + localCoCenterInfo.gatewayIp)
		req.Header.Set("X-Forwarded-For", buffer.String())
		http.Redirect(rw, req, nextCoCenterInfo.gatewayIp + nextCoCenterInfo.gatewayPort + req.RequestURI, 308)
	} else {
		buffer.WriteString(xForwardedFor + "," + localCoCenterInfo.gatewayIp)
		req.Header.Set("X-Forwarded-For", buffer.String())
		s.next.ServeHTTP(rw, req)
	}
}

func queryOptimalNetPath(optimalNetPathUrl string) (string, error) {
	optimalPathInfoResultMsg := new(optimalPathInfoMsg)
	client := &http.Client{}
	res, _ := client.Get(optimalNetPathUrl)
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	json.Unmarshal(body, optimalPathInfoResultMsg)
	return optimalPathInfoResultMsg.result, nil
}

func queryCoCenterInfo(coCenterUrl string, coCenterCode string) (coCenterInfo, error) {
	coCenterInfoResultMsg := new(coCenterInfoMsg)
	paramReader := strings.NewReader("")
	if coCenterCode != "" {
		q := url.Values{}
		q.Add("code", coCenterCode)
		paramReader = strings.NewReader(q.Encode())
	}
	req, _ := http.NewRequest("GET", coCenterUrl, paramReader)
	res, _ := myClient.Do(req)
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return coCenterInfo{}, err
	}
	json.Unmarshal(body, coCenterInfoResultMsg)
	return coCenterInfoResultMsg.result, nil
}

func fetchGatewayType(coCenterCode string, xRoutePath string) (int, string) {
	var gatewayType int
	var nextCoCenterCode string
	pathArray := strings.Split(xRoutePath, ",")
	index := indexOf(coCenterCode, pathArray)
	if index == 0 {
		nextCoCenterCode = pathArray[index + 1]
		gatewayType = 1
	} else if index == len(pathArray) - 1 {
		gatewayType = 3
	} else {
		nextCoCenterCode = pathArray[index + 1]
		gatewayType = 2
	}
	return gatewayType, nextCoCenterCode
}

func exceptionResponse(w http.ResponseWriter, r *http.Request) {
	var exceptionResponse = fmt.Sprintf("%d %s\n", http.StatusExpectationFailed, http.StatusText(http.StatusExpectationFailed))
	w.Header().Set(contentType, "text/plain")
	w.WriteHeader(http.StatusExpectationFailed)
	w.Write([]byte(exceptionResponse))
}

func indexOf(element string, data []string) (int) {
	for k, v := range data {
		if element == v {
			return k
		}
	}
	return -1    //not found.
}