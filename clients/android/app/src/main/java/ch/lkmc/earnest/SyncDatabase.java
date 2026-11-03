package ch.lkmc.earnest;

import androidx.room.Database;
import androidx.room.RoomDatabase;

@Database(entities = {SyncRow.class}, version = 1, exportSchema = false)
public abstract class SyncDatabase extends RoomDatabase {
    public abstract SyncDao sync();
}
