# Build em dois estagios.
#
# O primeiro estagio tem o compilador Go inteiro (~300 MB) e produz os dois
# binarios. O segundo copia so os binarios prontos para uma imagem minima.
# E por isso que a imagem final fica em poucos megabytes: Go gera binario
# estatico, entao nao precisa de runtime instalado.

FROM golang:1.27-alpine AS build

WORKDIR /src

# go.mod primeiro, sozinho: assim o Docker so refaz esta camada quando as
# dependencias mudam, e nao a cada alteracao de codigo.
COPY go.mod ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 forca binario 100% estatico, sem depender de bibliotecas C
# do sistema. E o que permite rodar na imagem alpine crua.
RUN CGO_ENABLED=0 go build -o /bin/servidor ./servidor && \
    CGO_ENABLED=0 go build -o /bin/cliente  ./cliente

# ---------------------------------------------------------------------------

FROM alpine:3.20

COPY --from=build /bin/servidor /bin/servidor
COPY --from=build /bin/cliente  /bin/cliente

# Onde o JSON fica gravado. O docker-compose monta um volume aqui para os
# dados sobreviverem ao container ser destruido.
RUN mkdir -p /dados
ENV VAIJUNTO_DADOS=/dados/vaijunto.json

EXPOSE 8080

CMD ["/bin/servidor"]
