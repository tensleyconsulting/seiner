package networkconnectivity

import (
	"testing"

	nc "google.golang.org/api/networkconnectivity/v1"
)

func TestFromRegionalEndpoint(t *testing.T) {
	t.Parallel()
	ep := fromRegionalEndpoint(&nc.RegionalEndpoint{
		Name:              "projects/net-host/locations/us-east4/regionalEndpoints/telemetry-rep",
		TargetGoogleApi:   "telemetry.us-east4.rep.googleapis.com",
		AccessType:        "REGIONAL",
		Address:           "192.168.16.27",
		Network:           "projects/net-host/global/networks/net-host-svc",
		Subnetwork:        "projects/net-host/regions/us-east4/subnetworks/net-host-svc-gke-use4",
		PscForwardingRule: "//compute.googleapis.com/projects/net-host/regions/us-east4/forwardingRules/rep-autogen-fr-telemetry",
		CreateTime:        "2026-11-05T16:43:42Z",
		UpdateTime:        "2026-11-05T16:44:28Z",
		Labels:            map[string]string{"managed-by": "terraform"},
	})
	if ep.Name != "telemetry-rep" || ep.Location != "us-east4" || ep.IPAddress != "192.168.16.27" || ep.AddressName != "" ||
		ep.Network != "net-host-svc" || ep.Subnetwork != "net-host-svc-gke-use4" ||
		ep.ForwardingRule != "rep-autogen-fr-telemetry" || ep.Created.IsZero() || ep.Updated.IsZero() {
		t.Fatalf("fromRegionalEndpoint (IP literal) = %+v", ep)
	}

	urlEP := fromRegionalEndpoint(&nc.RegionalEndpoint{
		Name:       "projects/p/locations/us-central1/regionalEndpoints/ep2",
		AccessType: "ACCESS_TYPE_UNSPECIFIED",
		Address:    "projects/p/regions/us-central1/addresses/reserved-addr",
		IpAddress:  "10.0.0.5",
	})
	if urlEP.AddressName != "reserved-addr" || urlEP.IPAddress != "10.0.0.5" || urlEP.AccessType != "" {
		t.Fatalf("fromRegionalEndpoint (URL address) = %+v", urlEP)
	}
}

func TestFromPolicyAndConnection(t *testing.T) {
	t.Parallel()
	pol := fromPolicy(&nc.ServiceConnectionPolicy{
		Name:           "projects/net-host/locations/us-east4/serviceConnectionPolicies/svc-sql",
		ServiceClass:   "google-cloud-sql",
		Infrastructure: "PSC",
		Network:        "projects/net-host/global/networks/net-host-svc",
		CreateTime:     "2026-10-01T19:23:49Z",
		UpdateTime:     "2026-10-02T00:45:23Z",
		PscConfig: &nc.PscConfig{
			Limit:                    8,
			ProducerInstanceLocation: "CUSTOM_RESOURCE_HIERARCHY_LEVELS",
			Subnetworks:              []string{"projects/net-host/regions/us-east4/subnetworks/svc-gke"},
			AllowedGoogleProducersResourceHierarchyLevel: []string{"folders/123456789012"},
		},
		PscConnections: []*nc.PscConnection{
			{
				PscConnectionId:        "10000000000000001",
				State:                  "ACTIVE",
				ConsumerForwardingRule: "https://www.googleapis.com/compute/v1/projects/net-svc/regions/us-east4/forwardingRules/sca-auto-fr-1",
				ConsumerAddress:        "https://www.googleapis.com/compute/v1/projects/net-svc/regions/us-east4/addresses/sca-auto-addr-1",
				SelectedSubnetwork:     "https://www.googleapis.com/compute/v1/projects/net-host/regions/us-east4/subnetworks/svc-gke",
				IpVersion:              "IP_VERSION_UNSPECIFIED",
			},
			{
				PscConnectionId: "99",
				State:           "FAILED",
				ErrorType:       "CONNECTION_ERROR_INTERNAL",
				Error:           &nc.GoogleRpcStatus{Message: "subnet full"},
				ErrorInfo:       &nc.GoogleRpcErrorInfo{Reason: "IP_EXHAUSTED"},
			},
		},
	})
	if pol.Name != "svc-sql" || pol.Location != "us-east4" || pol.ConnectionLimit != 8 ||
		len(pol.Subnetworks) != 1 || pol.Subnetworks[0] != "svc-gke" || len(pol.Connections) != 2 {
		t.Fatalf("fromPolicy = %+v", pol)
	}
	c0 := pol.Connections[0]
	if c0.ConsumerProject != "net-svc" || c0.ForwardingRule != "sca-auto-fr-1" ||
		c0.ConsumerAddress != "sca-auto-addr-1" || c0.Subnetwork != "svc-gke" || c0.IPVersion != "" {
		t.Fatalf("conn[0] = %+v", c0)
	}
	c1 := pol.Connections[1]
	if c1.Error != "IP_EXHAUSTED: subnet full" || c1.ErrorType != "CONNECTION_ERROR_INTERNAL" {
		t.Fatalf("conn[1] = %+v", c1)
	}
}
