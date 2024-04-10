package zerogravity

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	pb "github.com/zero-gravity-labs/zerog-data-avail/api/grpc/disperser"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const ZgMessageHeaderFlag byte = 0x0c

type BlobRequestParams struct {
	BatchHeaderHash []byte
	BlobIndex       uint32
}

type ZgConfig struct {
	Enable      bool   `koanf:"enable"`
	Address     string `koanf:"address"`
	MaxBlobSize int    `koanf:"max-blob-size"`
}

type ZgDA struct {
	Client pb.DisperserClient
	Cfg    ZgConfig
}

func NewZgDA(cfg ZgConfig) (*ZgDA, error) {
	conn, err := grpc.Dial(cfg.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))

	if err != nil {
		log.Error("Unable to connect zg server:", "error", err)
		return nil, err
	}

	c := pb.NewDisperserClient(conn)

	return &ZgDA{
		Client: c,
		Cfg:    cfg,
	}, nil
}

func (s *ZgDA) Store(ctx context.Context, seq []byte) ([]byte, error) {
	// message := make([][]BlobRequestParams, 0)

	// for i, seq := range batchesData {
	totalBlobSize := len(seq)
	// statusReplys := make([]*pb.BlobStatusReply, 0)
	requestParams := make([]BlobRequestParams, 0)
	if totalBlobSize > 0 {
		// var blobStatusReply *pb.BlobStatusReply
		log.Info("Store BatchL2Data", "data", hex.EncodeToString(seq), "len", totalBlobSize)

		for idx := 0; idx < totalBlobSize; idx += s.Cfg.MaxBlobSize {
			var endIdx int
			if totalBlobSize <= idx+s.Cfg.MaxBlobSize {
				endIdx = totalBlobSize
			} else {
				endIdx = idx + s.Cfg.MaxBlobSize
			}

			blob := pb.DisperseBlobRequest{
				Data: seq[idx:endIdx],
				SecurityParams: []*pb.SecurityParams{
					{
						QuorumId:           0,
						AdversaryThreshold: 0,
						QuorumThreshold:    0,
					},
				},
			}

			log.Info("Disperse blob range", "from", idx, "to", endIdx)
			blobReply, err := s.Client.DisperseBlob(ctx, &blob)
			if err != nil {
				log.Warn("Disperse blob error", "err", err)
				return nil, err
			}

			requestId := blobReply.GetRequestId()
			log.Info("Disperse request id", "id", hex.EncodeToString(requestId))
			for {
				statusReply, err := s.Client.GetBlobStatus(ctx, &pb.BlobStatusRequest{RequestId: requestId})

				if err != nil {
					log.Warn("Get blob status error", "err", err)
					return nil, err
				}
				log.Info("Blob status reply", "status", statusReply.GetStatus())

				if statusReply.GetStatus() == pb.BlobStatus_CONFIRMED {
					blobInfo := statusReply.GetInfo()
					blobVerificationProof := blobInfo.GetBlobVerificationProof()
					blobIndex := blobVerificationProof.GetBlobIndex()
					metadata := blobVerificationProof.GetBatchMetadata()
					batchHeaderHash := metadata.GetBatchHeaderHash()

					requestParams = append(requestParams, BlobRequestParams{
						BatchHeaderHash: batchHeaderHash,
						BlobIndex:       blobIndex,
					})

					break
				}

				time.Sleep(3 * time.Second)
			}
		}
	}

	rlpEncode, err := rlp.EncodeToBytes(&requestParams)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 0)
	buf = append(buf, ZgMessageHeaderFlag)
	buf = append(buf, rlpEncode...)
	return buf, nil
}

func (s *ZgDA) Read(ctx context.Context, requestParams []BlobRequestParams) ([]byte, error) {
	var blobData = make([]byte, 0)

	for _, requestParam := range requestParams {
		log.Info("Requesting data from zgDA", "param", requestParam)

		retrieveBlobReply, err := s.Client.RetrieveBlob(context.Background(), &pb.RetrieveBlobRequest{
			BatchHeaderHash: requestParam.BatchHeaderHash,
			BlobIndex:       requestParam.BlobIndex,
		})

		if err != nil {
			log.Error("Failed to retrieve blob", "error", err)
			return nil, err
		}

		blobData = append(blobData, retrieveBlobReply.GetData()...)
	}

	return blobData, nil
}
