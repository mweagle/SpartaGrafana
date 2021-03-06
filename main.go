// go:generate go run $GOPATH/src/github.com/mweagle/Sparta/aws/cloudformation/cli/describe.go --stackName SpartaHelloWorld --output .
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	sparta "github.com/mweagle/Sparta"
	spartaCF "github.com/mweagle/Sparta/aws/cloudformation"
	spartaS3 "github.com/mweagle/Sparta/aws/s3"
	"github.com/mweagle/SpartaGrafana/grafana"
	gocf "github.com/mweagle/go-cloudformation"
	"github.com/rcrowley/go-metrics"
	"github.com/rcrowley/go-metrics/exp"
	"github.com/vrischmann/go-metrics-influxdb"
)

////////////////////////////////////////////////////////////////////////////////
// CONSTANTS
const (
	GrafanaStackName = "GrafanaStack"

	GrafanaDNSNameOutput = "GrafanaPublicDNSName"
)

var grafanaStackArn = []gocf.Stringable{gocf.String("arn:aws:cloudformation:"),
	gocf.Ref("AWS::Region").String(),
	gocf.String(":"),
	gocf.Ref("AWS::AccountId").String(),
	gocf.String(":stack/"),
	gocf.String(GrafanaStackName),
	gocf.String("/*")}

// SSHKeyName is the SSH KeyName to use when provisioning new EC2 instance
var SSHKeyName string

////////////////////////////////////////////////////////////////////////////////
var helloWorldCounterMetric metrics.Counter

func init() {
	if "" != os.Getenv("AWS_LAMBDA_FUNCTION_VERSION") {
		// Go get the outputs of the GrafanaStack
		awsSession := session.New()
		cloudFormationSvc := cloudformation.New(awsSession)
		params := &cloudformation.DescribeStacksInput{
			StackName: aws.String(GrafanaStackName),
		}
		outputResults, outputResultsErr := cloudFormationSvc.DescribeStacks(params)
		if nil != outputResultsErr {
			fmt.Printf("ERROR: %s\n", outputResultsErr)
		} else if len(outputResults.Stacks) != 0 {
			stack := outputResults.Stacks[0]
			for _, eachOutput := range stack.Outputs {
				fmt.Printf("Testing GrafanaOutput: %+v\n", *eachOutput)
				if *eachOutput.OutputKey == GrafanaDNSNameOutput {
					influxHost := fmt.Sprintf("http://%s:8086", *eachOutput.OutputValue)
					fmt.Printf("Setting up InfluxDB publisher: %s\n", influxHost)

					r := rand.New(rand.NewSource(time.Now().UnixNano()))
					tags := map[string]string{
						"lambda": fmt.Sprintf("λ-%d", r.Int63()),
					}

					go influxdb.InfluxDBWithTags(
						metrics.DefaultRegistry, // metrics registry
						time.Second*5,           // interval
						influxHost,              // the InfluxDB url
						"lambda",                // your InfluxDB database
						"",                      // your InfluxDB user
						"",                      // your InfluxDB password,
						tags,
					)
				}
			}
		}
		helloWorldCounterMetric = metrics.NewCounter()
		metrics.Register("HelloWorld", helloWorldCounterMetric)
		metrics.RegisterRuntimeMemStats(metrics.DefaultRegistry)
	}
}

// Standard AWS λ function
func helloWorld(event *json.RawMessage,
	context *sparta.LambdaContext,
	w http.ResponseWriter,
	logger *logrus.Logger) {

	helloWorldCounterMetric.Inc(1)
	fmt.Fprint(w, "Hello World")
}

// PostBuildHook is the hook used to annotate the template
func PostBuildHook(context map[string]interface{},
	serviceName string,
	S3Bucket string,
	buildID string,
	awsSession *session.Session,
	noop bool,
	logger *logrus.Logger) error {

	// Get the grafana template && make sure it exists...
	grafanaTemplate, grafanaTemplateErr := grafana.Stack(SSHKeyName, GrafanaDNSNameOutput)
	if nil != grafanaTemplateErr {
		return grafanaTemplateErr
	}

	if !noop {
		// Go make the stack...
		keyName := spartaCF.CloudFormationResourceName("Grafana", "Grafana")
		tempFile, tempFileErr := ioutil.TempFile("", "grafana")
		if nil != tempFileErr {
			tempFile.Close()
			return tempFileErr
		}

		// Save the template...
		cfTemplate, cfTemplateErr := json.Marshal(grafanaTemplate)
		if nil != cfTemplateErr {
			return cfTemplateErr
		}
		_, writeErr := tempFile.Write(cfTemplate)
		if nil != writeErr {
			return writeErr
		}
		tempFile.Close()

		uploadLocation, uploadURLErr := spartaS3.UploadLocalFileToS3(tempFile.Name(),
			awsSession,
			S3Bucket,
			keyName,
			logger)
		if nil != uploadURLErr {
			return uploadURLErr
		}

		stack, stackErr := spartaCF.ConvergeStackState(GrafanaStackName,
			grafanaTemplate,
			uploadLocation,
			nil,
			time.Now(),
			awsSession,
			logger)
		if nil != stackErr {
			return stackErr
		}

		logger.WithFields(logrus.Fields{
			"GrafanaStack": *stack,
		}).Info("Created Grafana Stack")

	} else {
		logger.Info("Bypassing Grafana stack due to -noop flag")
	}
	// Go fetch the stack outputs and stuff them into this archive...
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Main
func main() {

	// And add the SSHKeyName option to the provision step
	sparta.CommandLineOptions.Provision.Flags().StringVarP(&SSHKeyName,
		"key",
		"k",
		"",
		"SSH Key Name to use for EC2 instances")

	exp.Exp(metrics.DefaultRegistry)

	hooks := &sparta.WorkflowHooks{
		Context:   map[string]interface{}{},
		PostBuild: PostBuildHook,
	}

	// Provision an IAM::Role as part of this application
	var iamLambdaRole = sparta.IAMRoleDefinition{}
	iamLambdaRole.Privileges = append(iamLambdaRole.Privileges, sparta.IAMRolePrivilege{
		Actions:  []string{"cloudformation:DescribeStacks"},
		Resource: gocf.Join("", grafanaStackArn...),
	})
	var lambdaFunctions []*sparta.LambdaAWSInfo
	lambdaFn := sparta.NewLambda(iamLambdaRole,
		helloWorld,
		nil)

	stage := sparta.NewStage("dev")
	apiGateway := sparta.NewAPIGateway("SpartaGrafana", stage)
	apiGatewayResource, _ := apiGateway.NewResource("/hello/grafana", lambdaFn)
	apiGatewayResource.NewMethod("GET", http.StatusOK)

	// Provision the EC2 hosting Grafana
	lambdaFn.Decorator = func(serviceName string,
		lambdaResourceName string,
		lambdaResource gocf.LambdaFunction,
		resourceMetadata map[string]interface{},
		S3Bucket string,
		S3Key string,
		buildID string,
		template *gocf.Template,
		context map[string]interface{},
		logger *logrus.Logger) error {

		resourceMetadata["GrafanaHost"] = gocf.ImportValue(gocf.String(GrafanaDNSNameOutput))
		return nil
	}
	lambdaFunctions = append(lambdaFunctions, lambdaFn)
	stackName := spartaCF.UserScopedStackName("SpartaGrafanaPublisher")
	err := sparta.MainEx(stackName,
		fmt.Sprintf("Sparta application that provisions and publishes to a Grafana instance"),
		lambdaFunctions,
		apiGateway,
		nil,
		hooks,
		false)
	if err != nil {
		os.Exit(1)
	}
}
