package job

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net"
	"net/http"
	"time"

	"appengine"
	"appengine/socket"
	"appengine/urlfetch"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"

	"config"
)

var (
	dreamServerAmi          = config.Get("DREAMPICS_DREAMSERVER_AMI")
	dreamServerInstanceType = config.Get("DREAMPICS_DREAMSERVER_INSTANCE_TYPE")
	awsSecurityGroup        = config.Get("AWS_SECURITY_GROUP")
)

func init() {
	if dreamServerAmi == "" {
		panic("DREAMPICS_DREAMSERVER_AMI environmental variable missing.")
	}
	if dreamServerInstanceType == "" {
		panic("DREAMPICS_DREAMSERVER_INSTANCE_TYPE environmental variable missing.")
	}
	if awsSecurityGroup == "" {
		panic("AWS_SECURITY_GROUP environmental variable missing.")
	}
}

type Instance struct {

	// The ID of the EC2 instance.
	// Empty means this instance isn't launched yet.
	ID string

	// The authentication code we use to authenticate to our instance.
	AuthCode string

	// Our PEM-encoded TLS certificate for this instance.
	// We will refuse to talk to the server
	// if its TLS certificate does not match.
	Certificate []byte

	// The PEM-encoded TLS private key for this instance.
	// Only kept during the launch process,
	// to enable us to supply the same key when retrying launches.
	PrivateKey []byte

	// The time at which we sent a launch request to Amazon.
	LaunchTime time.Time

	// The public IP address associated with this instance.
	IP string
}

// Launch a new instance, setting ID and launch time.
// This must be called after AuthCode, Certificate, and PrivateKey
// are set and saved, with a client token for idempotency.
func (i *Instance) launch(c appengine.Context, clientToken string) (err error) {

	var awsConfig = &aws.Config{
		HTTPClient: urlfetch.Client(c),
	}
	svc := ec2.New(awsConfig)

	// Build auth data to pass to instance.
	userData := struct {
		AuthCode       string `json:"auth_code"`
		SslCertificate []byte `json:"ssl_certificate"`
		SslPrivateKey  []byte `json:"ssl_private_key"`
	}{
		i.AuthCode,
		i.Certificate,
		i.PrivateKey,
	}
	userDataJson, err := json.Marshal(userData)
	if err != nil {
		return
	}
	userDataStr := base64.StdEncoding.EncodeToString(userDataJson)

	// Create full parameters for instance.
	params := &ec2.RunInstancesInput{
		ClientToken:    aws.String(clientToken),
		ImageID:        aws.String(dreamServerAmi),
		InstanceType:   aws.String(dreamServerInstanceType),
		MinCount:       aws.Long(1),
		MaxCount:       aws.Long(1),
		UserData:       aws.String(userDataStr),
		SecurityGroups: []*string{aws.String(awsSecurityGroup)},
	}

	var runResult *ec2.Reservation
	runResult, err = svc.RunInstances(params)
	if err != nil {
		return
	}

	i.ID = *runResult.Instances[0].InstanceID
	i.LaunchTime = time.Now()

	// Stop storing the private key now we've
	// passed it to the instance and no longer need it.
	i.PrivateKey = nil

	return
}

func (i *Instance) toPoolInstance(c appengine.Context) (p *PoolInstance) {

	p = &PoolInstance{
		Instance:    *i,
		PoolAddTime: time.Now(),
	}

	return
}

// Make a HTTP GET request to our instance.
// Same response semantics as http.Client's Get.
func (i *Instance) get(c appengine.Context, pathAndQuery string) (resp *http.Response, err error) {

	// Try to get the public IP for this instance.
	// If it's unavailable, then we immediately fail the request.
	ip, err := i.publicIP(c)
	if err != nil {
		c.Infof("Instance IP lookup failed: " + err.Error())
		return nil, err
	}

	// Construct our HTTP client for talking to the instance.
	client, err := i.httpClient(c)
	if err != nil {
		c.Infof("Instance HTTP Client setup failed: " + err.Error())
		return nil, err
	}

	// Make the request.
	resp, err = client.Get("https://" + ip + ":8080/" + pathAndQuery)
	if err != nil {
		c.Infof("Instance HTTP GET failed: " + err.Error())
		return nil, err
	}

	return resp, nil
}

// Make a HTTP POST request to our instance.
// Same response semantics as http.Client's PostForm.
func (i *Instance) postFile(c appengine.Context, pathAndQuery, filename string, file []byte) (
	resp *http.Response, err error) {

	data, contentType, err := encodeFile(filename, file)
	if err != nil {
		return nil, err
	}

	// Try to get the public IP for this instance.
	// If it's unavailable, then we immediately fail the request.
	ip, err := i.publicIP(c)
	if err != nil {
		c.Infof("Instance IP lookup failed: " + err.Error())
		return nil, err
	}

	// Construct our HTTP client for talking to the instance.
	client, err := i.httpClient(c)
	if err != nil {
		c.Infof("Instance HTTP Client setup failed: " + err.Error())
		return nil, err
	}

	// Make the request.
	resp, err = client.Post("https://" + ip + ":8080/" + pathAndQuery, contentType, data)
	if err != nil {
		c.Infof("Instance HTTP POST failed: " + err.Error())
		return nil, err
	}

	return resp, nil
}

func (i *Instance) httpClient(c appengine.Context) (client *http.Client, err error) {

	// Create TLS settings for expected TLS certificate.
	// We rely on knowing the cert we expect in advance,
	// rather than a usual certificate chain.
	cpool := x509.NewCertPool()
	if !cpool.AppendCertsFromPEM(i.Certificate) {
		return nil, errors.New("Could not add certificate to pool.")
	}
	tlsConfig := &tls.Config{
		RootCAs: cpool,
		ServerName: "dreamserver",
	}

	// Create a HTTP client which uses the sockets API rather
	// than the urlfetch API, since we need custom TLS config,
	// in order to be secure.
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		Dial: func(network, addr string) (net.Conn, error) {
			conn, err := socket.Dial(c, network, addr)
			if err != nil {
				return nil, err
			}

			// If we don't do this step,
			// sockets time out after five seconds.
			// This is not conductive to our longer requests.
			err = conn.SetReadDeadline(time.Now().Add(50 * time.Minute))
			if err != nil {
				return nil, err
			}
			err = conn.SetWriteDeadline(time.Now().Add(50 * time.Minute))
			if err != nil {
				return nil, err
			}

			return conn, nil
		},
	}
	client = &http.Client{
		Transport: transport,
	}

	return
}

func (i *Instance) publicIP(c appengine.Context) (ip string, err error) {

	if i.IP != "" {
		return i.IP, nil
	}

	var awsConfig = &aws.Config{
		HTTPClient: urlfetch.Client(c),
	}
	svc := ec2.New(awsConfig)

	params := &ec2.DescribeInstancesInput{
		InstanceIDs: []*string{ aws.String(i.ID) },
	}

	descResult, err := svc.DescribeInstances(params)
	if err != nil {
		return "", err
	}
	if len(descResult.Reservations) == 0 {
		return "", errors.New("No such instance ID found on AWS; terminated or still starting?")
	}
	if descResult.Reservations[0].Instances[0].PublicIPAddress == nil {
		return "", errors.New("No public IP found for instance; still starting up?")
	}

	return *descResult.Reservations[0].Instances[0].PublicIPAddress, nil
}

func (i *Instance) terminate(c appengine.Context) error {

	var awsConfig = &aws.Config{
		HTTPClient: urlfetch.Client(c),
	}
	svc := ec2.New(awsConfig)

	params := &ec2.TerminateInstancesInput{
		InstanceIDs: []*string{aws.String(i.ID)},
	}

	_, err := svc.TerminateInstances(params)
	return err
}

func encodeFile(field string, file []byte) (data *bytes.Buffer, contentType string, err error) {

	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)

	fileWriter, err := w.CreateFormFile(field, field)
	if err != nil {
		return nil, "", err
	}
	if _, err = fileWriter.Write(file); err != nil {
		return nil, "", err
	}
	w.Close()

	return buf, w.FormDataContentType(), nil
}
