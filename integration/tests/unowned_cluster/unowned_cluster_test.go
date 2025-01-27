// +build integration

package unowned_clusters

import (
	"fmt"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"github.com/weaveworks/eksctl/pkg/eks"

	cfn "github.com/aws/aws-sdk-go/service/cloudformation"

	"github.com/aws/aws-sdk-go/aws"

	api "github.com/weaveworks/eksctl/pkg/apis/eksctl.io/v1alpha5"

	awseks "github.com/aws/aws-sdk-go/service/eks"
	. "github.com/weaveworks/eksctl/integration/runner"
	"github.com/weaveworks/eksctl/integration/tests"
	"github.com/weaveworks/eksctl/pkg/testutils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var params *tests.Params

func init() {
	// Call testing.Init() prior to tests.NewParams(), as otherwise -test.* will not be recognised. See also: https://golang.org/doc/go1.13#testing
	testing.Init()
	params = tests.NewParams("unowned_clusters")
}

func TestE2E(t *testing.T) {
	testutils.RegisterAndRun(t)
}

var _ = Describe("(Integration) [non-eksctl cluster & nodegroup support]", func() {
	Context("Get, upgrade & delete cluster/nodegroups", func() {
		var (
			clusterName, stackName, ng1, ng2 string
			ctl                              api.ClusterProvider
		)

		BeforeEach(func() {
			ng1 = "ng-1"
			ng2 = "ng-2"
			// "unowned_clusters" lead to names longer than allowed for CF stacks
			clusterName = params.NewClusterName("uc")
			stackName = fmt.Sprintf("it-%s", clusterName)
			cfg := &api.ClusterConfig{
				Metadata: &api.ClusterMeta{
					Name:   params.ClusterName,
					Region: params.Region,
				},
			}
			ctl = eks.New(&api.ProviderConfig{Region: params.Region}, cfg).Provider
			createClusterWithNodegroups(clusterName, stackName, ng1, ng2, ctl)
		})

		AfterEach(func() {
			deleteStack(stackName, ctl)
		})

		It("should work", func() {
			By("Getting clusters")
			cmd := params.EksctlGetCmd.
				WithArgs(
					"clusters",
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfullyWithOutputStringLines(
				ContainElement(ContainSubstring(clusterName)),
			))

			By("Getting nodegroups")
			cmd = params.EksctlGetCmd.
				WithArgs(
					"nodegroups",
					"--cluster", clusterName,
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfullyWithOutputStringLines(
				ContainElement(ContainSubstring("ng-1")),
			))
			Expect(cmd).To(RunSuccessfullyWithOutputStringLines(
				ContainElement(ContainSubstring("ng-2")),
			))

			By("Enabling OIDC")
			cmd = params.EksctlUtilsCmd.
				WithArgs(
					"associate-iam-oidc-provider",
					"--name", clusterName,
					"--approve",
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfully())
			By("Creating an IAMServiceAccount")
			cmd = params.EksctlCreateCmd.
				WithArgs(
					"iamserviceaccount",
					"--cluster", clusterName,
					"--name", "test-sa",
					"--namespace", "default",
					"--attach-policy-arn",
					"arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
					"--approve",
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfully())
			By("Getting IAMServiceAccounts")
			cmd = params.EksctlGetCmd.
				WithArgs(
					"iamserviceaccounts",
					"--cluster", clusterName,
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfullyWithOutputStringLines(
				ContainElement(ContainSubstring("test-sa")),
			))

			By("Upgrading the cluster")
			cmd = params.EksctlUpgradeCmd.
				WithArgs(
					"cluster",
					"--name", clusterName,
					"--version", "1.18",
					"--approve",
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfully())

			By("Creating an addon")
			cmd = params.EksctlCreateCmd.
				WithArgs(
					"addon",
					"--cluster", clusterName,
					"--name", "vpc-cni",
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfully())

			By("Getting an addon")
			cmd = params.EksctlGetCmd.
				WithArgs(
					"addons",
					"--cluster", clusterName,
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfullyWithOutputStringLines(
				ContainElement(ContainSubstring("vpc-cni")),
			))

			By("Upgrading one of the nodegroups")
			cmd = params.EksctlUpgradeCmd.
				WithArgs(
					"nodegroup",
					"--name", ng1,
					"--cluster", clusterName,
					"--kubernetes-version", "1.18",
					"--wait",
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfully())

			By("Deleting a nodegroup")
			cmd = params.EksctlDeleteCmd.
				WithArgs(
					"nodegroup",
					"--name", ng2,
					"--cluster", clusterName,
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfully())

			By("Deleting the cluster")
			cmd = params.EksctlDeleteCmd.
				WithArgs(
					"cluster",
					"--name", clusterName,
					"--verbose", "2",
				)
			Expect(cmd).To(RunSuccessfully())
		})
	})
})

func createClusterWithNodegroups(clusterName, stackName, ng1, ng2 string, ctl api.ClusterProvider) {
	subnets, clusterRoleArn, nodeRoleArn := createVPCAndRole(stackName, ctl)

	_, err := ctl.EKS().CreateCluster(&awseks.CreateClusterInput{
		Name: &clusterName,
		ResourcesVpcConfig: &awseks.VpcConfigRequest{
			SubnetIds: aws.StringSlice(subnets),
		},
		RoleArn: &clusterRoleArn,
		Version: aws.String("1.17"),
	})
	Expect(err).NotTo(HaveOccurred())
	Eventually(func() string {
		out, err := ctl.EKS().DescribeCluster(&awseks.DescribeClusterInput{
			Name: &clusterName,
		})
		Expect(err).NotTo(HaveOccurred())
		return *out.Cluster.Status
	}, time.Minute*20, time.Second*30).Should(Equal("ACTIVE"))

	_, err = ctl.EKS().CreateNodegroup(&awseks.CreateNodegroupInput{
		NodegroupName: &ng1,
		ClusterName:   &clusterName,
		NodeRole:      &nodeRoleArn,
		Subnets:       aws.StringSlice(subnets),
		ScalingConfig: &awseks.NodegroupScalingConfig{
			MaxSize:     aws.Int64(1),
			DesiredSize: aws.Int64(1),
			MinSize:     aws.Int64(1),
		},
	})
	Expect(err).NotTo(HaveOccurred())
	_, err = ctl.EKS().CreateNodegroup(&awseks.CreateNodegroupInput{
		NodegroupName: &ng2,
		ClusterName:   &clusterName,
		NodeRole:      &nodeRoleArn,
		Subnets:       aws.StringSlice(subnets),
		ScalingConfig: &awseks.NodegroupScalingConfig{
			MaxSize:     aws.Int64(1),
			DesiredSize: aws.Int64(1),
			MinSize:     aws.Int64(1),
		},
	})
	Expect(err).NotTo(HaveOccurred())

	Eventually(func() string {
		out, err := ctl.EKS().DescribeNodegroup(&awseks.DescribeNodegroupInput{
			ClusterName:   &clusterName,
			NodegroupName: &ng1,
		})
		Expect(err).NotTo(HaveOccurred())
		return *out.Nodegroup.Status
	}, time.Minute*20, time.Second*30).Should(Equal("ACTIVE"))

	Eventually(func() string {
		out, err := ctl.EKS().DescribeNodegroup(&awseks.DescribeNodegroupInput{
			ClusterName:   &clusterName,
			NodegroupName: &ng2,
		})
		Expect(err).NotTo(HaveOccurred())
		return *out.Nodegroup.Status
	}, time.Minute*20, time.Second*30).Should(Equal("ACTIVE"))
}

func createVPCAndRole(stackName string, ctl api.ClusterProvider) ([]string, string, string) {
	templateBody, err := ioutil.ReadFile("cf-template.yaml")
	Expect(err).NotTo(HaveOccurred())
	createStackInput := &cfn.CreateStackInput{
		StackName: &stackName,
	}
	createStackInput.SetTemplateBody(string(templateBody))
	createStackInput.SetCapabilities(aws.StringSlice([]string{cfn.CapabilityCapabilityIam}))
	createStackInput.SetCapabilities(aws.StringSlice([]string{cfn.CapabilityCapabilityNamedIam}))

	_, err = ctl.CloudFormation().CreateStack(createStackInput)
	Expect(err).NotTo(HaveOccurred())

	var describeStackOut *cfn.DescribeStacksOutput
	Eventually(func() string {
		describeStackOut, err = ctl.CloudFormation().DescribeStacks(&cfn.DescribeStacksInput{
			StackName: &stackName,
		})
		Expect(err).NotTo(HaveOccurred())
		return *describeStackOut.Stacks[0].StackStatus
	}, time.Minute*10, time.Second*15).Should(Equal(cfn.StackStatusCreateComplete))

	var clusterRoleARN, nodeRoleARN string
	var subnets []string
	for _, output := range describeStackOut.Stacks[0].Outputs {
		switch *output.OutputKey {
		case "ClusterRoleARN":
			clusterRoleARN = *output.OutputValue
		case "NodeRoleARN":
			nodeRoleARN = *output.OutputValue
		case "SubnetIds":
			subnets = strings.Split(*output.OutputValue, ",")
		}
	}

	return subnets, clusterRoleARN, nodeRoleARN
}

func deleteStack(stackName string, ctl api.ClusterProvider) {
	deleteStackInput := &cfn.DeleteStackInput{
		StackName: &stackName,
	}

	_, err := ctl.CloudFormation().DeleteStack(deleteStackInput)
	Expect(err).NotTo(HaveOccurred())
}
