package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/journeymidnight/yig/circuitbreak"
	"github.com/journeymidnight/yig/helper"
	"github.com/journeymidnight/yig/iam/common"
	"github.com/journeymidnight/yig/mods"
)

const pluginName = "wocloud_iam"

//The variable MUST be named as Exported.
//the code in yig-plugin will lookup this symbol
var Exported = mods.YigPlugin{
	Name:       pluginName,
	PluginType: mods.IAM_PLUGIN,
	Create:     GetWocloudIamClient,
}

type AccessKeyItem struct {
	ProjectId    string `json:"projectId"`
	Name         string `json:"name"`
	AccessKey    string `json:"accessKey"`
	AccessSecret string `json:"accessSecret"`
	Status       string `json:"status"`
	Updated      string `json:"updated"`
}

type Query struct {
	Action     string   `json:"action"`
	ProjectId  string   `json:"projectId,omitempty"`
	AccessKeys []string `json:"accessKeys,omitempty"`
	Offset     int      `json:"offset,omitempty"`
	//	Limit      int      `json:"limit"`
}

type QueryResp struct {
	Limit        int             `json:"limit"`
	Total        int             `json:"total"`
	Offset       int             `json:"offset"`
	AccessKeySet []AccessKeyItem `json:"accessKeySet"`
}

type QueryRespAll struct {
	Message string    `json:"message"`
	Data    QueryResp `json:"data"`
	RetCode int       `json:"retCode"`
}

type Client struct {
	httpClient  *circuitbreak.CircuitClient
	iamEndpoint string
	iamKey      string
	iamSecret   string
}

func (a *Client) GetKeysByUid(uid string) (credentials []common.Credential, err error) {
	var slog = helper.Logger
	var query Query
	var offset int = 0
	var total int = 0
	query.Action = "DescribeAccessKeys"
	query.ProjectId = uid
	for {
		query.Offset = offset
		b, err := json.Marshal(query)
		if err != nil {
			slog.Println(5, "json err:", err)
			return credentials, err
		}
		request, _ := http.NewRequest("POST", a.iamEndpoint, strings.NewReader(string(b)))
		request.Header.Set("X-Le-Key", "key")
		request.Header.Set("X-Le-Secret", "secret")
		slog.Println(10, "replay request:", request, string(b))
		response, err := a.httpClient.Do(request)
		if err != nil {
			slog.Println(5, "replay histroy send request failed", err)
			return credentials, err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			slog.Println(5, "QueryHistory to IAM failed as status != 200")
			return credentials, fmt.Errorf("QueryHistory to IAM failed as status != 200")
		}

		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			slog.Println(5, "QueryHistory ioutil.ReadAll failed")
			return credentials, fmt.Errorf("QueryHistory ioutil.ReadAll failed")
		}

		var queryRetAll QueryRespAll
		err = json.Unmarshal(body, &queryRetAll)
		if err != nil {
			return credentials, errors.New("Decode QueryRespAll failed")
		}
		if queryRetAll.RetCode != 0 {
			return credentials, errors.New("Query to IAM failed as RetCode != 0")
		}
		for _, value := range queryRetAll.Data.AccessKeySet {
			credential := common.Credential{}
			credential.UserId = value.ProjectId
			credential.DisplayName = value.Name
			credential.AccessKeyID = value.AccessKey
			credential.SecretAccessKey = value.AccessSecret
			credential.AllowOtherUserAccess = false
			credentials = append(credentials, credential)
		}
		total = queryRetAll.Data.Total
		count := len(queryRetAll.Data.AccessKeySet)
		if queryRetAll.Data.Offset+count < total {
			offset = queryRetAll.Data.Offset + count
		} else {
			break
		}
	}
	return
}

func (a *Client) GetCredential(accessKey string) (credential common.Credential, err error) {
	var slog = helper.Logger
	var query Query
	query.Action = "DescribeAccessKeys"
	query.AccessKeys = append(query.AccessKeys, accessKey)

	b, err := json.Marshal(query)
	if err != nil {
		return credential, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		select {
		case <-time.After(10 * time.Second):
			slog.Println(5, "send iam request timeout, over 10s")
		case <-ctx.Done():
			slog.Println(20, ctx.Err()) // prints "context deadline exceeded"
		}
	}()

	request, err := http.NewRequest("POST", a.iamEndpoint, bytes.NewReader(b))
	if err != nil {
		return credential, err
	}

	request.Header.Set("X-Le-Key", a.iamKey)
	request.Header.Set("X-Le-Secret", a.iamSecret)
	request.Header.Set("content-type", "application/json")
	request = request.WithContext(ctx)
	response, err := a.httpClient.Do(request)
	if err != nil {
		return credential, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return credential, errors.New("Query to IAM failed as status != 200")
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return credential, err
	}
	slog.Println(10, "iam:", a.iamEndpoint)
	slog.Println(10, "request:", string(b))
	slog.Println(10, "response:", string(body))

	var queryRetAll QueryRespAll
	err = json.Unmarshal(body, &queryRetAll)
	if err != nil {
		return credential, errors.New("Decode QueryHistoryResp failed")
	}
	if queryRetAll.RetCode != 0 {
		return credential, errors.New("Query to IAM failed as RetCode != 0")
	}

	if queryRetAll.Data.Total > 0 {
		credential.UserId = queryRetAll.Data.AccessKeySet[0].ProjectId
		credential.DisplayName = queryRetAll.Data.AccessKeySet[0].Name
		credential.AccessKeyID = queryRetAll.Data.AccessKeySet[0].AccessKey
		credential.SecretAccessKey = queryRetAll.Data.AccessKeySet[0].AccessSecret
		return credential, nil
	} else {
		return credential, common.ErrAccessKeyNotExist
	}
	return credential, nil
}

func GetWocloudIamClient(config map[string]interface{}) (interface{}, error) {

	helper.Logger.Printf(10, "Get plugin config: %v\n", config)

	c := Client{
		httpClient:  circuitbreak.NewCircuitClient(),
		iamEndpoint: config["iam_endpoint"].(string),
		iamKey:      config["iam_key"].(string),
		iamSecret:   config["iam_secret"].(string),
	}

	return interface{}(c), nil
}
