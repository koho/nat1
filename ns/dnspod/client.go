package dnspod

import (
	"log"

	"github.com/miekg/dns"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"

	"github.com/koho/nat1/ns"
	"github.com/koho/nat1/pb"
)

type Client struct {
	*dnspod.Client
}

func New(secret *pb.DNSPod) (*Client, error) {
	credential := common.NewCredential(
		secret.SecretId,
		secret.SecretKey,
	)
	client, err := dnspod.NewClient(credential, "", profile.NewClientProfile())
	if err != nil {
		return nil, err
	}
	return &Client{client}, nil
}

func (c *Client) SetA(domain string, value string) error {
	recordId, err := c.getRecordId(domain, "A")
	if err, ok := err.(*errors.TencentCloudSDKError); ok && err.Code != "ResourceNotFound.NoDataOfRecord" {
		return err
	}

	if recordId == nil {
		return c.createRecord(domain, "A", value, nil)
	} else {
		return c.updateRecord(recordId, domain, "A", value, nil)
	}
}

func (c *Client) SetSVCB(domain string, priority int, target string, params map[string]string, https bool) error {
	t := "SVCB"
	if https {
		t = "HTTPS"
	}
	recordId, err := c.getRecordId(domain, t)
	if err, ok := err.(*errors.TencentCloudSDKError); ok && err.Code != "ResourceNotFound.NoDataOfRecord" {
		return err
	}

	value := dns.Fqdn(target)
	for k, v := range params {
		value += " " + k + "=\"" + v + "\""
	}

	if recordId == nil {
		return c.createRecord(domain, t, value, common.Uint64Ptr(uint64(priority)))
	} else {
		return c.updateRecord(recordId, domain, t, value, common.Uint64Ptr(uint64(priority)))
	}
}

func (c *Client) createRecord(domain string, t string, value string, mx *uint64) error {
	request := dnspod.NewCreateRecordRequest()
	request.SubDomain, request.Domain = ns.SplitDomainPtr(domain)
	request.RecordType = common.StringPtr(t)
	request.RecordLine = common.StringPtr("默认")
	request.MX = mx
	request.Value = common.StringPtr(value)
	response, err := c.CreateRecord(request)
	if err != nil {
		return err
	}
	log.Printf("[%s] %s", domain, response.ToJsonString())
	return nil
}

func (c *Client) updateRecord(recordId *uint64, domain string, t string, value string, mx *uint64) error {
	request := dnspod.NewModifyRecordRequest()
	request.SubDomain, request.Domain = ns.SplitDomainPtr(domain)
	request.MX = mx
	request.Value = common.StringPtr(value)
	request.RecordId = recordId
	request.RecordLine = common.StringPtr("默认")
	request.RecordType = common.StringPtr(t)
	response, err := c.ModifyRecord(request)
	if err != nil {
		return err
	}
	log.Printf("[%s] %s", domain, response.ToJsonString())
	return nil
}

func (c *Client) getRecordId(domain string, t string) (*uint64, error) {
	request := dnspod.NewDescribeRecordListRequest()
	request.Subdomain, request.Domain = ns.SplitDomainPtr(domain)
	request.RecordType = common.StringPtr(t)
	request.Limit = common.Uint64Ptr(1)

	response, err := c.DescribeRecordList(request)
	if err != nil {
		return nil, err
	}
	if len(response.Response.RecordList) > 0 {
		return response.Response.RecordList[0].RecordId, nil
	}
	return nil, nil
}
