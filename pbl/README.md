# Vaijunto

Sistema de caronas compartilhadas sobre sockets TCP. Servidor central, clientes
de motorista e de passageiro, tudo em Go sem nenhuma dependência externa.

## Rodar

```
cd pbl
go run ./servidor      # terminal 1
go run ./cliente       # terminal 2 (pode abrir vários)
```

Ao abrir, o cliente oferece **entrar** ou **criar conta**. Na criação você
escolhe se é motorista ou passageiro, e já entra com a conta nova.

Também existem usuários de teste prontos, senha `123` para todos:

| Login | Tipo |
|---|---|
| `ana`, `davi` | motorista |
| `bruno`, `carla` | passageiro |

## O que cada um faz

| Usuário | Ações |
|---|---|
| Motorista | cadastrar carona, listar as suas caronas, ver os passageiros de cada trecho, cancelar carona |
| Passageiro | buscar viagens (diretas e com uma conexão), reservar, pagar, listar reservas, cancelar reserva |

## Testes

```
go test ./...          # regras, atomicidade, concorrência e queda de cliente
go test -race ./...    # o mesmo, com detector de corrida (exige Go 64 bits)
```

### Teste de carga

Clientes reais, cada um com a sua conexão TCP, disputando os mesmos assentos
ao mesmo tempo contra o servidor rodando:

```
go run ./servidor                               # terminal 1
go run ./carga                                  # terminal 2: 200 clientes, 10 lugares
go run ./carga -clientes 1000 -assentos 50
go run ./carga -servidor 192.168.0.10:8080      # contra outra máquina
```

Ao final ele confere, só pelo protocolo:

- nenhum lugar vendido duas vezes;
- todos os lugares disponíveis foram vendidos;
- cada reserva aceita tem número único;
- nenhum itinerário ficou pela metade (a disputa usa duas caronas, e cada
  reserva aceita precisa ocupar as duas).

E mostra a latência da reserva (mínima, média, p50, p95 e máxima).

## Docker

```
docker compose up -d --build      # sobe o servidor
docker compose run --rm cliente   # abre um cliente
docker compose down               # derruba
```

## Variáveis de ambiente

| Variável | Padrão | Para quê |
|---|---|---|
| `VAIJUNTO_RESERVA` | `10m` | prazo para pagar antes do assento voltar |
| `VAIJUNTO_VARREDURA` | `30s` | frequência da checagem de reservas vencidas |
| `VAIJUNTO_DADOS` | `vaijunto.json` | arquivo onde o estado é gravado |
| `VAIJUNTO_SERVIDOR` | `localhost:8080` | onde o cliente e a carga procuram o servidor |

Para demonstrar a expiração sem esperar 10 minutos:

```
VAIJUNTO_RESERVA=30s VAIJUNTO_VARREDURA=5s go run ./servidor
```

No PowerShell:

```
$env:VAIJUNTO_RESERVA="30s"; $env:VAIJUNTO_VARREDURA="5s"; go run ./servidor
```

## Estrutura

```
protocolo/   mensagens trocadas na rede (Pedido, Resposta, Item, Opcao)
dados/       estado do sistema, mutex e regras de negócio
servidor/    aceita conexões, roteia ações, expira reservas
cliente/     menus de terminal
carga/       teste de carga com clientes reais via socket
```

## Como funciona

**Protocolo.** Uma linha de texto é uma mensagem, e o conteúdo é JSON. É isso
que resolve o TCP não ter fronteira de mensagem: quem lê sabe que a mensagem
acabou ao encontrar o `\n`.

**Queda de cliente.** O servidor separa dois tipos de erro de leitura. Se a
linha chegou mas o JSON está errado (`protocolo.ErrMensagemInvalida`), ele
responde o erro e continua atendendo. Qualquer outro erro é da conexão: o
cliente fechou, caiu de forma abrupta ou a rede sumiu. Nesse caso a goroutine
daquele cliente termina, e os demais seguem normalmente.

**Cadastro de usuário.** `CadastrarUsuario` faz a checagem "login já existe" e
a escrita no mapa dentro do mesmo `Lock`. Se a checagem ficasse fora, duas
pessoas registrando o mesmo login ao mesmo tempo passariam ambas pela
verificação antes de qualquer uma escrever.

**Concorrência.** Uma goroutine por cliente conectado, mais uma goroutine de
fundo que expira reservas. Todas mexem no mesmo `Banco`, protegido por um
único `sync.Mutex`. A regra do pacote `dados` é: função pública trava, função
interna nunca trava.

**Disponibilidade por trecho.** Cada carona tem `Ocupados []int`, um contador
por trecho. Uma rota com 3 cidades tem 2 trechos. Reservar Feira→Ilhéus
incrementa os dois; reservar Salvador→Ilhéus incrementa só o segundo. Por isso
um mesmo assento pode estar livre num trecho e ocupado em outro.

**Reserva atômica.** `Reservar` roda em duas fases dentro de um único `Lock`:
primeiro confere todos os trechos pedidos sem alterar nada, e só aplica se
todos passarem. Como o mutex não é solto entre as fases, ninguém se intromete
no meio. É o que garante o "pegou um trecho não fica sem o outro".

**Cancelamentos.** Cancelar uma reserva devolve os assentos de todos os
trechos dela. Cancelar uma carona desfaz todas as reservas ativas que passam
por ela — e, se uma dessas reservas era uma conexão com outra carona, a
reserva cai inteira e o assento volta nas duas. Liberar só a parte da carona
cancelada deixaria o passageiro com meia viagem.

**Expiração.** Reserva nasce `pendente` com prazo. Uma goroutine com
`time.Ticker` devolve os assentos das que venceram. Reserva paga nunca expira.

**Persistência.** Todo o estado é gravado em JSON a cada alteração, de dentro
da seção crítica — assim o arquivo nunca pega o estado pela metade. Sem SGBD,
como o enunciado exige.

## Demonstração

Suba o servidor com `VAIJUNTO_RESERVA=30s VAIJUNTO_VARREDURA=5s` e siga:

1. `ana` cadastra carona `Feira, Salvador, Ilheus`, data `2026-09-14`,
   **1 assento**, R$30 por trecho
2. `bruno` busca Feira → Salvador e reserva
3. `carla` busca os três pares e vê a disponibilidade por trecho:

   | Busca | Resultado |
   |---|---|
   | Salvador → Ilhéus | 1 opção, o trecho seguinte está livre |
   | Feira → Salvador | nenhuma, o trecho está ocupado |
   | Feira → Ilhéus | nenhuma, precisaria dos dois |

4. ninguém paga; em até 35s o servidor loga `expirei 1 reserva(s) nao paga(s)`
5. `carla` busca de novo: o assento voltou
6. `carla` reserva e paga; passado o prazo, continua `paga`
7. `ana` abre "passageiros" da carona e vê a `carla` no trecho dela; depois
   cancela a carona, e a reserva da `carla` aparece como `cancelada` com o
   motivo

Para mostrar a conexão, peça a `davi` que cadastre `Ilheus, Porto Seguro` na
mesma data e busque Feira → Porto Seguro: aparece um itinerário com duas
caronas, que é reservado de forma atômica.

Para mostrar a concorrência sob carga, rode `go run ./carga -clientes 1000`
com o servidor de pé.
