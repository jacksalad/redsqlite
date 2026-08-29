/** 无主键表行定位列名（与后端 identifier.RowidName 对应）。 */
export const ROWID_COLUMN = "__rowid__";

/** 表重建进度事件名（与后端 services.RedesignProgressEvent 对应，V3.0）。 */
export const REDESIGN_PROGRESS_EVENT = "redesign:progress";

/** 跨连接传输进度事件名（与后端 services.TransferProgressEvent 对应，V3.0）。 */
export const TRANSFER_PROGRESS_EVENT = "transfer:progress";
