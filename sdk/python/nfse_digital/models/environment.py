from enum import Enum


class Environment(str, Enum):
    HOMOLOGATION = "homologation"
    PRODUCTION = "production"

    def __str__(self) -> str:
        return str(self.value)
