package platform

type StockConflictError struct{ Current []StockLevel }

func (e *StockConflictError) Error() string { return "Dữ liệu tồn kho đã thay đổi" }
func (e *StockConflictError) Unwrap() error { return Conflict() }
