package platform

import (
	"context"
	"dicho/internal/kernel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

type Quantity = kernel.Quantity
type Money = kernel.Money
type Principal struct {
	UserID, SessionID, SystemRole string
	TokenVersion                  int64
}
type Clock interface{ Now() time.Time }
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

type Querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Stock struct {
	LotID, FoodID, UnitID, Dimension string
	Quantity                         Quantity
	ExpiresOn                        time.Time
	Version                          int64
}
type Consumption struct {
	FoodID, LotID string
	Quantity      Quantity
	AllowExpired  bool
}
type StockInput struct {
	ID                               string
	ItemID, FoodID, UnitID, Location string
	Quantity                         Quantity
	ExpiresOn                        time.Time
	PurchasedOn                      time.Time
	TotalCost                        *int64
}
type FoodUnit struct {
	FoodID, Name, UnitID, Dimension string
	Factor                          Quantity
}
type Ingredient struct {
	FoodID, UnitID, Dimension string
	Quantity                  Quantity
	Optional                  bool
}
type MissingLine struct {
	FoodID, UnitID, Name string
	Quantity             Quantity
	NeededOn             time.Time
}
type StockPort interface {
	Available(context.Context, pgx.Tx, string, time.Time) ([]Stock, error)
	Consume(context.Context, pgx.Tx, string, string, []Consumption) error
}
type StockInPort interface {
	AddLots(context.Context, pgx.Tx, string, string, []StockInput) ([]string, error)
}
type CatalogPort interface {
	ValidateFood(context.Context, pgx.Tx, string, string, string) (FoodUnit, error)
}
type CatalogCreator interface {
	CreatePrivateFood(context.Context, pgx.Tx, string, string, string, string) (FoodUnit, error)
}
type RecipePort interface {
	Ingredients(context.Context, pgx.Tx, string, string, Quantity) ([]Ingredient, error)
}
type ListComposer interface {
	Compose(context.Context, pgx.Tx, string, string, string, []MissingLine) (string, error)
}
type MemberLeaveHook func(context.Context, pgx.Tx, string, string) error
type RecipeDeleteHook func(context.Context, pgx.Tx, string, string) error
type AuthenticateFunc func(context.Context, string) (Principal, error)

type PendingSupply struct {
	FoodID, UnitID, Dimension string
	Quantity                  Quantity
	AvailableOn               time.Time
}
type PendingSupplyPort interface {
	Pending(context.Context, pgx.Tx, string, time.Time, time.Time) ([]PendingSupply, error)
}

type Composition struct {
	TargetListID, Name string
	PlannedDate        time.Time
	Lines              []MissingLine
}
type PlannedListComposer interface {
	ComposePlanned(context.Context, pgx.Tx, string, string, Composition) (string, error)
}

type SearchKey struct{ Name, Type, ID string }
type LotSearchQuery struct {
	Query                                string
	CategoryIDs, Locations, ExpiryStates []string
	After                                SearchKey
	Limit                                int
}
type LotSearchPage struct {
	Items                                        []map[string]any
	Total                                        int
	CategoryCounts, LocationCounts, ExpiryCounts map[string]int
}
type LotSearchPagePort interface {
	SearchLotsPage(context.Context, pgx.Tx, string, string, LotSearchQuery) (LotSearchPage, error)
}
