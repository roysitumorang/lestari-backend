package httpx

import (
	"github.com/gofiber/fiber/v3"
)

// Principal is the authenticated user plus the store/branch the request acts on.
type Principal struct {
	UserID      string
	StoreID     string
	BranchID    string // active branch (X-Branch-Id or the user's default)
	HeadOffice  string // head-office branch of the store
	Role        string
	Name        string
	Email       string
	Permissions []string
	Plugins     []string
}

const principalKey = "lestari.principal"

func SetPrincipal(c fiber.Ctx, p *Principal) { c.Locals(principalKey, p) }

// P returns the principal; routes behind the auth middleware always have one.
func P(c fiber.Ctx) *Principal {
	if p, ok := c.Locals(principalKey).(*Principal); ok {
		return p
	}
	return &Principal{}
}

func (p *Principal) IsOwner() bool { return p.Role == "owner" }

func (p *Principal) Can(perm string) bool {
	if p.Role == "owner" || p.Role == "administrator" {
		return true
	}
	for _, x := range p.Permissions {
		if x == perm {
			return true
		}
	}
	return false
}

func (p *Principal) HasPlugin(code string) bool {
	for _, x := range p.Plugins {
		if x == code {
			return true
		}
	}
	return false
}
