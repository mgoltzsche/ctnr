// See https://github.com/containers/image/blob/master/docs/policy.json.md
package store

import (
	"github.com/containers/image/signature"
	"github.com/containers/image/types"
)

type TrustPolicyContext interface {
	Policy() (*signature.PolicyContext, error)
}

type trustPolicyFileContext struct {
	file   string
	policy *signature.PolicyContext
}

func TrustPolicyFromFile(file string) TrustPolicyContext {
	return &trustPolicyFileContext{file, nil}
}

func (c *trustPolicyFileContext) Policy() (p *signature.PolicyContext, err error) {
	if c.policy != nil {
		return c.policy, nil
	}

	policy, err := signature.NewPolicyFromFile(c.file)
	if err != nil {
		return nil, err
	}
	p, err = signature.NewPolicyContext(policy)
	if err == nil {
		c.policy = p
	}
	return
}

type trustPolicyDefault struct {
	ctx    *types.SystemContext
	policy *signature.PolicyContext
}

func TrustPolicyDefault(ctx *types.SystemContext) TrustPolicyContext {
	return &trustPolicyDefault{ctx, nil}
}

func (c *trustPolicyDefault) Policy() (p *signature.PolicyContext, err error) {
	if c.policy != nil {
		return c.policy, nil
	}

	policy, err := signature.DefaultPolicy(c.ctx)
	if err != nil {
		return nil, err
	}
	p, err = signature.NewPolicyContext(policy)
	if err == nil {
		c.policy = p
	}
	return
}

type trustPolicyInsecure struct{}

func TrustPolicyInsecure() TrustPolicyContext {
	return &trustPolicyInsecure{}
}

func (c *trustPolicyInsecure) Policy() (p *signature.PolicyContext, err error) {
	return signature.NewPolicyContext(&signature.Policy{
		Default: []signature.PolicyRequirement{
			signature.NewPRInsecureAcceptAnything(),
		},
	})
}
