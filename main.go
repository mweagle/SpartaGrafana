// go:generate go run $GOPATH/src/github.com/mweagle/Sparta/aws/cloudformation/cli/describe.go --stackName SpartaHelloWorld --output .
package main

import (
	"encoding/json"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	gocf "github.com/crewjam/go-cloudformation"
	sparta "github.com/mweagle/Sparta"
	spartaCF "github.com/mweagle/Sparta/aws/cloudformation"
	"github.com/mweagle/SpartaGrafana/grafana"
	"github.com/rcrowley/go-metrics"
	"github.com/rcrowley/go-metrics/exp"
	"github.com/vrischmann/go-metrics-influxdb"
	"net/http"
	"os"
	"time"
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
		// Go get the outputs of the
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
					go influxdb.InfluxDB(
						metrics.DefaultRegistry, // metrics registry
						time.Second*5,           // interval
						influxHost,              // the InfluxDB url
						"lambda",                // your InfluxDB database
						"",                      // your InfluxDB user
						"",                      // your InfluxDB password
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
		stack, stackErr := spartaCF.ConvergeStackState(GrafanaStackName,
			grafanaTemplate,
			S3Bucket,
			keyName,
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
	err := sparta.MainEx("SpartaGrafanaPublisher",
		fmt.Sprintf("Sparta application that provisions and publishes to a Grafana instance"),
		lambdaFunctions,
		nil,
		nil,
		hooks)
	if err != nil {
		os.Exit(1)
	}
}
