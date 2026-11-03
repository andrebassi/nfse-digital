from enum import Enum


class CancelRequestReason(str, Enum):
    ERRO_EMISSAO = "erro_emissao"
    OUTROS = "outros"
    SERVICO_NAO_PRESTADO = "servico_nao_prestado"

    def __str__(self) -> str:
        return str(self.value)
