from enum import Enum


class InvoiceStatus(str, Enum):
    AUTHORIZED = "authorized"
    CANCELLED = "cancelled"
    PENDING = "pending"
    REJECTED = "rejected"

    def __str__(self) -> str:
        return str(self.value)
