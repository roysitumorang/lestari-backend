package models

const (
	PPOBKindPrepaid  = "prepaid"
	PPOBKindPostpaid = "postpaid"
)

// PPOBItem is one product of the PPOB price list a new branch starts with.
type PPOBItem struct {
	Kind,
	Category,
	Provider,
	Code,
	Name string
	BasePrice,
	SellingPrice,
	BaseAdminFee,
	AdminFee *float64
}

func prepaidItem(category, provider, code, name string, basePrice, sellingPrice float64) PPOBItem {
	return PPOBItem{Kind: PPOBKindPrepaid, Category: category, Provider: provider, Code: code, Name: name, BasePrice: &basePrice, SellingPrice: &sellingPrice}
}

func postpaidItem(category, code, name string, baseAdminFee float64) PPOBItem {
	return PPOBItem{Kind: PPOBKindPostpaid, Category: category, Code: code, Name: name, BaseAdminFee: &baseAdminFee, AdminFee: new(baseAdminFee + 1000)}
}

// PPOBCatalog is the starter price list given to every new branch.
var PPOBCatalog = []PPOBItem{
	prepaidItem("pulsa", "Telkomsel", "TSEL10", "Telkomsel 10.000", 10250, 12000),
	prepaidItem("pulsa", "Telkomsel", "TSEL25", "Telkomsel 25.000", 24900, 27000),
	prepaidItem("pulsa", "Telkomsel", "TSEL50", "Telkomsel 50.000", 49500, 52000),
	prepaidItem("pulsa", "Indosat", "ISAT10", "Indosat 10.000", 10600, 12000),
	prepaidItem("pulsa", "XL", "XL10", "XL 10.000", 10500, 12000),
	prepaidItem("pulsa", "Tri", "TRI10", "Tri 10.000", 10300, 12000),
	prepaidItem("data", "Telkomsel", "TSELD5GB", "Telkomsel Data 5 GB 30 days", 52000, 56000),
	prepaidItem("data", "XL", "XLD10GB", "XL Data 10 GB 30 days", 64000, 69000),
	prepaidItem("pln_token", "PLN", "PLN20", "PLN Token 20.000", 20500, 22500),
	prepaidItem("pln_token", "PLN", "PLN50", "PLN Token 50.000", 50500, 52500),
	prepaidItem("pln_token", "PLN", "PLN100", "PLN Token 100.000", 100500, 102500),
	prepaidItem("e_money", "GoPay", "GOPAY50", "GoPay 50.000", 51000, 53000),
	prepaidItem("e_money", "OVO", "OVO50", "OVO 50.000", 51000, 53000),
	prepaidItem("e_money", "DANA", "DANA50", "DANA 50.000", 50800, 52500),
	prepaidItem("game_voucher", "Mobile Legends", "ML86", "Mobile Legends 86 Diamonds", 22000, 25000),
	prepaidItem("game_voucher", "Free Fire", "FF100", "Free Fire 100 Diamonds", 14500, 17000),
	prepaidItem("streaming", "Vidio", "VIDIO30", "Vidio Premier 30 days", 29000, 33000),
	postpaidItem("pln_postpaid", "PLNPOST", "PLN Postpaid", 2500),
	postpaidItem("pdam", "PDAM", "PDAM Water", 2500),
	postpaidItem("bpjs", "BPJSKES", "BPJS Kesehatan", 2500),
	postpaidItem("telkom", "TELKOM", "Telkom / IndiHome", 2500),
	postpaidItem("gas_postpaid", "PGN", "PGN Gas", 2500),
	postpaidItem("multifinance", "FIF", "FIF Group", 3000),
	postpaidItem("internet_tv", "BIZNET", "Biznet Home", 2500),
}
