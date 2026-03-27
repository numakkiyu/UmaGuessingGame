import type { GameState } from "@/lib/validation/schemas";

export type RoomRealtimeRole = "host" | "spectator";

export type RoomRealtimeClientMessage =
  | {
      type: "subscribe";
      roomCode: string;
      role: RoomRealtimeRole;
    }
  | {
      type: "heartbeat";
      roomCode: string;
      role: RoomRealtimeRole;
      sentAt: string;
    }
  | {
      type: "rtc-offer" | "rtc-answer";
      roomCode: string;
      connectionId: string;
      sdp: RTCSessionDescriptionInit;
    }
  | {
      type: "rtc-ice";
      roomCode: string;
      connectionId: string;
      candidate: RTCIceCandidateInit;
    };

export type RoomRealtimeServerMessage =
  | {
      type: "room-state";
      roomCode: string;
      gameState: GameState;
    }
  | {
      type: "heartbeat-ack";
      roomCode: string;
      receivedAt: string;
    }
  | {
      type: "presence";
      roomCode: string;
      hostOnline: boolean;
      spectatorCount: number;
    }
  | {
      type: "rtc-offer" | "rtc-answer";
      roomCode: string;
      connectionId: string;
      sdp: RTCSessionDescriptionInit;
    }
  | {
      type: "rtc-ice";
      roomCode: string;
      connectionId: string;
      candidate: RTCIceCandidateInit;
    };

