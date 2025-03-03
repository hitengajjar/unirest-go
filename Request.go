package unirest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/apimatic/form"
	"github.com/satori/go.uuid"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"crypto/tls"
)

type Request struct {
	httpClient     *http.Client
	transport      *http.Transport
	cookies        []*http.Cookie
	connectTimeout int
	httpMethod     HttpMethod             //HTTP method for the outgoing request
	url            string                 //Url for the outgoing request
	headers        map[string]interface{} //Headers for the outgoing request
	body           interface{}            //Parameters for raw body type request
	username       string                 //Basic auth password
	password       string                 //Basic auth password
}

func NewRequest(method HttpMethod, url string,
	headers map[string]interface{}, parameters interface{},
	username string, password string) *Request {

	request := makeRequest(method, url, headers, username, password)
	request.body = parameters
	return request
}

func makeRequest(method HttpMethod, url string,
	headers map[string]interface{},
	username string, password string) *Request {

	//prepare a new request object
	request := new(Request)

	//prepare the transport layer
	request.connectTimeout = -1
	request.transport = &http.Transport{DisableKeepAlives: false, MaxIdleConnsPerHost: 2}
	request.httpClient = &http.Client{
		Transport: request.transport,
	}

	//perpare the request parameters
	request.httpClient.Timeout = time.Duration(15 * time.Minute)
	request.httpMethod = method
	request.url = url
	request.headers = headers
	request.username = username
	request.password = password

	return request
}

func (me *Request) PerformRequest(skipVerify bool) (*http.Response, error) {
	var req *http.Request
	var err error
	var method = me.httpMethod.ToString()

	//encode body and parameters to the request
	if me.body != nil {
		req, err = me.encodeBody(method)
	} else {
		req, err = http.NewRequest(method, me.url, nil)
	}
	if err != nil {
		return nil, err
	}
	//load headers
	me.encodeHeaders(req)

	//set timeout values
	me.httpClient.Transport.(*http.Transport).TLSHandshakeTimeout += 2 * time.Minute
	me.httpClient.Transport.(*http.Transport).ResponseHeaderTimeout = 15 * time.Minute
	
	//set skipVerify SSL
	me.httpClient.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: skipVerify}

	//perform the underlying http request
	res, err := me.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (me *Request) encodeHeaders(req *http.Request) {
	//encode headers and basic auth fields
	for key, value := range me.headers {
		strVal := ToString(value, "")
		if len(strVal) > 0 {
			req.Header.Set(key, strVal)
		}
	}

	//append basic auth headers
	if len(me.username) > 1 || len(me.password) > 1 {
		authToken := base64.StdEncoding.EncodeToString([]byte(me.username + ":" + me.password))
		req.Header.Set("Authorization", "Basic "+authToken)
	}
}

//Decides whether to encode using form parameters with multipart/url encoded or send as a raw body
func (me *Request) encodeBody(method string) (*http.Request, error) {
	var req *http.Request
	var err error
	if params, ok := me.body.(map[string]interface{}); ok {
		//encode parameters using form encoder with brackets
		param, _ := form.EncodeToValues(params, form.BRACKET)
		//Check if the parameters contain a file
		for key, _ := range param {
			if key == "[file]" {
				return me.encodeMultiPartFormData(method, param)
			}
		}
		return me.encodeUrlEncodedFormData(method, param)
	} else {
		return me.encodeRawBody(method)
	}
	return req, err
}

func (me *Request) encodeUrlEncodedFormData(method string, param url.Values) (*http.Request, error) {
	paramValues := url.Values{}
	for key, val := range param {
		paramValues.Add(key, ToString(val[0], ""))
	}
	//creating request
	req, err := http.NewRequest(method, me.url, strings.NewReader(paramValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return req, err
}

func (me *Request) encodeRawBody(method string) (*http.Request, error) {
	isString := false
	var bodyBytes []byte
	var err error
	//Serializes all parameters except string which is sent raw
	if reflect.ValueOf(me.body).Kind() != reflect.String {
		bodyBytes, err = json.Marshal(me.body)
		if err != nil {
			return nil, errors.New("Invalid JSON in the query")
		}
	} else {
		isString = true
		bodyBytes = []byte(me.body.(string))
	}

	reader := bytes.NewReader(bodyBytes)
	req, err := http.NewRequest(method, me.url, reader)
	req.Header.Set("Content-Length", strconv.Itoa(len(string(bodyBytes))))
	if(!isString){
		req.Header.Set("Content-Type", "application/json; charset=utf-8")		
	} 
	return req, err
}

func (me *Request) encodeMultiPartFormData(method string, param url.Values) (*http.Request, error) {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	//Adds the file data
	for key, val := range param {
		if key == "[file]" {
			part, _ := writer.CreateFormFile("file", "file_encode")
			part.Write([]byte(val[0]))
		}
	}
	//Adds additional parameters
	for key, val := range param {
		if key != "[file]" {
			writer.WriteField(key, ToString(val[0], ""))
		}
	}
	writer.Close()

	req, err := http.NewRequest(method, me.url, body)
	req.Header.Add("Content-Type", writer.FormDataContentType())
	return req, err
}

/**
 * Uses reflection to check if the given value is a zero value
 * @param   v    The given value for the finding the string representation
 * @return	True, if the value is a zero value
 */
func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Func, reflect.Map, reflect.Slice:
		return v.IsNil()
	case reflect.Array:
		z := true
		for i := 0; i < v.Len(); i++ {
			z = z && isZero(v.Index(i))
		}
		return z
	case reflect.Struct:
		z := true
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				z = z && isZero(v.Field(i))
			}
		}
		return z
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return false //numeric and bool zeros are not to be detected
	}
	// Compare other types directly:
	z := reflect.Zero(v.Type())
	result := v.Interface() == z.Interface()

	return result
}

/**
 * Uses reflection to get string representation of a given data
 * @param   data    The given data for the finding the string representation
 * @param   dVal    The default value string to use if the given value is nil
 */
func ToString(data interface{}, dVal string) string {
	if data == nil {
		return dVal
	} else if str, ok := data.(string); ok {
		return str
	}
	value := reflect.ValueOf(data)
	if isZero(value) {
		return dVal
	}
	return toString(value)
}

/**
 * Uses reflection to get string representation of a given value
 * @param   value   The refelcted value to find the string represenation for
 */
func toString(value reflect.Value) string {
	valueKind := value.Kind()
	if valueKind == reflect.Ptr {
		value = value.Elem()
	}

	valueType := value.Type().String()
	switch valueType {
	case "bool":
		return strconv.FormatBool(value.Bool())
	case "int", "int8", "int32", "int64",
		"uint", "uint8", "uint32", "uint64":
		return strconv.FormatInt(value.Int(), 10)
	case "float32":
		return strconv.FormatFloat(value.Float(), 'f', -1, 32)
	case "float64":
		return strconv.FormatFloat(value.Float(), 'f', -1, 64)
	case "string":
		return value.String()
	case "time.Time":
		return value.Interface().(time.Time).String()
	case "uuid.UUID":
		return value.Interface().(uuid.UUID).String()
	default:
		jsonValue, _ := json.Marshal(value.Interface())
		return string(jsonValue)
	}
}
