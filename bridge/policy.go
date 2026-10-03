package bridge

import (
	"errors"
	"time"
)

type permissions map[string]bool

func permissionSet(names []string, valid func(string) bool) (permissions, error) {
	set := permissions{}
	for _, name := range names {
		if !valid(name) {
			return nil, errors.New("invalid host permission")
		}
		set[name] = true
	}
	return set, nil
}

type accessPolicy struct {
	grant      Grant
	operations permissions
	documents  permissions
}

func (g Grant) valid() bool {
	return g.PrincipalID != "" && g.Environment == "personal" &&
		idPattern.MatchString(g.ProjectID) && revisionPattern.MatchString(g.PolicyRevision) &&
		!g.ExpiresAt.IsZero()
}

func (p *accessPolicy) configure(g Grant) error {
	if !g.valid() {
		return errors.New("invalid synthetic host grant")
	}
	p.grant = g
	var err error
	p.operations, err = permissionSet(g.Operations, func(s string) bool { return operations[s] })
	if err != nil {
		return err
	}
	p.documents, err = permissionSet(g.DocumentIDs, idPattern.MatchString)
	return err
}

func (p accessPolicy) authorize(identity *Identity, now time.Time) string {
	switch {
	case identity == nil:
		return "UNAUTHENTICATED"
	case identity.PrincipalID != p.grant.PrincipalID || identity.Environment != p.grant.Environment:
		return "DENIED"
	case !p.grant.Enabled:
		return "PROJECT_DISABLED"
	case !now.Before(p.grant.ExpiresAt):
		return "DENIED"
	}
	return ""
}

func (p accessPolicy) allows(r request) bool {
	return r.ProjectID == p.grant.ProjectID && p.operations[r.Operation]
}
