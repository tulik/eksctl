package fargate

import (
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/kris-nova/logger"
	"github.com/pkg/errors"
	api "github.com/weaveworks/eksctl/pkg/apis/eksctl.io/v1alpha5"
	"github.com/weaveworks/eksctl/pkg/utils/strings"
)

// ReadProfile reads the Fargate profile corresponding to the provided name if
// it exists.
func (m *Manager) ReadProfile(name string) (*api.FargateProfile, error) {
	out, err := m.api.DescribeFargateProfile(describeRequest(m.clusterName, name))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get Fargate profile %q", name)
	}
	logger.Debug("Fargate profile: describe request: received: %#v", out)
	return toFargateProfile(out.FargateProfile), nil
}

// ReadProfiles reads all existing Fargate profiles.
func (m *Manager) ReadProfiles() ([]*api.FargateProfile, error) {
	names, err := m.ListProfiles()
	if err != nil {
		return nil, err
	}
	profiles := []*api.FargateProfile{}
	for _, name := range names {
		profile, err := m.ReadProfile(*name)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

// ListProfiles lists all existing Fargate profiles.
func (m *Manager) ListProfiles() ([]*string, error) {
	out, err := m.api.ListFargateProfiles(listRequest(m.clusterName))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get Fargate profile(s) for cluster %q", m.clusterName)
	}
	logger.Debug("Fargate profile: list request: received %v profile(s): %#v", len(out.FargateProfileNames), out)
	return out.FargateProfileNames, nil
}

func listRequest(clusterName string) *eks.ListFargateProfilesInput {
	request := &eks.ListFargateProfilesInput{
		ClusterName: &clusterName,
	}
	logger.Debug("Fargate profile: list request: sending: %#v", request)
	return request
}

func toFargateProfile(in *eks.FargateProfile) *api.FargateProfile {
	return &api.FargateProfile{
		Name:                *in.FargateProfileName,
		Selectors:           toSelectors(in.Selectors),
		PodExecutionRoleARN: strings.EmptyIfNil(in.PodExecutionRoleArn),
		Subnets:             strings.ToValuesArray(in.Subnets),
		Tags:                strings.ToValuesMap(in.Tags),
		Status:              *in.Status,
	}
}
